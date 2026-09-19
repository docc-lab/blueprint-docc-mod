#!/usr/bin/env python3
"""Tomislav-RetCtx: what fraction of REQUESTS end up with a damaged trace?

Span drop rates understate nothing and overstate nothing, but they answer the wrong
question: a user reads traces, not spans. This measures, per offered rate:

  vanilla  : fraction of traces missing at least one span (any loss damages the trace)
  bridges  : fraction of traces missing at least one CHECKPOINT. Losing a non-checkpoint
             is by design -- the truss on the surviving checkpoints still reconstructs the
             path -- so only checkpoint loss is damage.

Both come from the traces Jaeger actually stored (settled-traces.json.gz, <=100 per point),
not from a model of the counters. Three facts make this exact enough to trust:

  1. The workload is a single deterministic call graph: every ComposePost trace has exactly
     23 spans at 23 distinct (service, operation, kind) positions, verified at every
     zero-loss point. So "missing" is knowable per trace without ground-truth bookkeeping.
  2. An exported span is self-labelling: the collector attaches `_br` (the full chain) to a
     checkpoint and `_d`/`_o` to a non-checkpoint, so every span that survived says which
     class it was in.
  3. Only the class of a span that did NOT survive is unknown. For those we use the measured
     class-conditional drop rates on that span's own collector, so the estimate accounts for
     the priority processor preferring to drop non-checkpoints:

        P(checkpoint | dropped, node) =        q*d_hp
                                        ---------------------
                                        q*d_hp + (1-q)*d_lp

     with q = P(this position is a checkpoint), measured per position at a zero-loss point,
     and d_hp/d_lp the node's checkpoint / non-checkpoint drop rates at this point.
     P(trace damaged) = 1 - prod over missing positions (1 - P(checkpoint | dropped)).

For vanilla there is no class split and the formula collapses to the direct count: any missing
span damages the trace, so that column is a direct measurement with no model in it.

Survivorship: Jaeger finds a trace by its wrk2api process, so a trace that lost BOTH of its
wrk2api spans is invisible to the query and cannot be counted. The report prints how often
each wrk2api span survived so that bias is visible rather than assumed away.
"""
import argparse
from collections import Counter, defaultdict
import gzip
import json
from pathlib import Path
import statistics

CHECKPOINT_TAG = '_br'


def load_traces(point):
    path = point / 'settled-traces.json.gz'
    if not path.exists():
        return []
    with gzip.open(path, 'rt') as stream:
        return json.load(stream)['data']


def position(span, processes):
    service = processes.get(span['processID'], {}).get('serviceName', '?')
    service = service.split(':')[-1].split('_service')[0]
    kind = next((t['value'] for t in span.get('tags', []) if t['key'] == 'span.kind'), '?')
    return service, span['operationName'], kind


def trace_positions(trace):
    processes = trace.get('processes', {})
    out = {}
    for span in trace['spans']:
        key = position(span, processes)
        out[key] = any(t['key'] == CHECKPOINT_TAG for t in span.get('tags', []))
    return out


def collector_nodes(point):
    """service name -> node, and collector pod -> node, from this point's pod listing."""
    pods = json.loads((point / 'after/pods.json').read_text())['items']
    service_node, collector_node = {}, {}
    for pod in pods:
        name, node = pod['metadata']['name'], pod['spec'].get('nodeName')
        if name.startswith('otelcol-'):
            collector_node[name] = node
        elif '-service-' in name:
            service_node[name.split('-service-')[0]] = node
    return service_node, collector_node


def drop_rates(point, collector_node, bridge):
    """node -> (checkpoint drop rate, non-checkpoint drop rate). Vanilla has no split."""
    before = json.loads((point / 'before/snapshot.json').read_text())
    after = json.loads((point / 'after/snapshot.json').read_text())
    rates = {}
    if not bridge:
        for name, new in after['collectors'].items():
            old = before['collectors'].get(name)
            if old is None:
                continue
            refused = new.get('otelcol_receiver_refused_spans_total', 0) - old.get('otelcol_receiver_refused_spans_total', 0)
            accepted = new.get('otelcol_receiver_accepted_spans_total', 0) - old.get('otelcol_receiver_accepted_spans_total', 0)
            total = refused + accepted
            rate = refused / total if total else 0.
            rates[collector_node[name]] = (rate, rate)
        return rates
    for name, new in after['sdk'].items():
        metrics = new.get('_processor_metrics')
        old = before['sdk'].get(name, {}).get('_processor_metrics')
        if not name.startswith('otelcol-') or metrics is None or old is None:
            continue
        d = {k: metrics.get(k, 0) - old.get(k, 0) for k in ('hp_refused', 'hp_admitted', 'lp_refused', 'lp_admitted')}
        hp, lp = d['hp_refused'] + d['hp_admitted'], d['lp_refused'] + d['lp_admitted']
        rates[collector_node[name]] = (d['hp_refused'] / hp if hp else 0., d['lp_refused'] / lp if lp else 0.)
    return rates


def reference(case_dir):
    """Positions of a complete trace, and P(position is a checkpoint), from the lowest-rate
    point whose traces are all complete. Asserts the topology really is deterministic."""
    for point in sorted(case_dir.glob('rate-*')):
        traces = load_traces(point)
        if not traces:
            continue
        sizes = Counter(len(t['spans']) for t in traces)
        if len(sizes) != 1:
            continue
        positions = [trace_positions(t) for t in traces]
        keys = set(positions[0])
        if any(set(p) != keys for p in positions) or len(keys) != len(positions[0]):
            continue
        prior = {k: sum(p[k] for p in positions) / len(positions) for k in keys}
        return point.name, keys, prior
    raise SystemExit(f'no complete-trace reference point in {case_dir}')


def analyse(case_dir, bridge):
    ref_name, keys, prior = reference(case_dir)
    rows = []
    for point in sorted(case_dir.glob('rate-*')):
        traces = load_traces(point)
        if not traces:
            continue
        result = json.loads((point / 'result.json').read_text())
        service_node, collector_node = collector_nodes(point)
        rates = drop_rates(point, collector_node, bridge)
        damaged, incomplete, wrk_spans = 0., 0, Counter()
        for trace in traces:
            present = trace_positions(trace)
            missing = keys - set(present)
            wrk_spans[sum(1 for k in present if k[0] == 'wrk2api')] += 1
            if missing:
                incomplete += 1
            if not bridge:
                damaged += bool(missing)
                continue
            intact = 1.
            for key in missing:
                node = service_node.get(key[0])
                d_hp, d_lp = rates.get(node, (0., 0.))
                q = prior[key]
                denominator = q * d_hp + (1 - q) * d_lp
                intact *= 1 - (q * d_hp / denominator if denominator else 0.)
            damaged += 1 - intact
        rows.append({'offered_rps': result['offered_rps'], 'completed_rps': result['completed_rps'],
                     'repetition': result['repetition'], 'traces': len(traces),
                     'incomplete_pct': 100 * incomplete / len(traces),
                     'damaged_pct': 100 * damaged / len(traces),
                     'wrk2api_spans_surviving': dict(wrk_spans)})
    return ref_name, rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, action='append', required=True,
                        help='experiment root; repeatable (bridges root + the root holding vanilla)')
    parser.add_argument('--json', type=Path, default=None, help='write the full per-point table here')
    args = parser.parse_args()

    table = defaultdict(lambda: defaultdict(list))
    refs = {}
    for root in args.out:
        for case_dir in sorted((root.resolve() / 'run').iterdir()):
            if not (case_dir / 'complete.json').exists():
                continue
            kind = case_dir.name[3:]
            if kind == 'nt':
                continue
            ref_name, rows = analyse(case_dir, bridge=kind != 'v')
            refs[kind] = ref_name
            for row in rows:
                table[kind][row['offered_rps']].append(row)

    out = {}
    for kind in sorted(table):
        print(f"== {kind}   (complete-trace reference: {refs[kind]}, "
              f"{'any span missing' if kind == 'v' else 'checkpoint missing'})")
        print(f"   {'offered':>7} {'completed':>9} {'traces':>6} {'incomplete %':>12} {'DAMAGED %':>10}")
        for rate in sorted(table[kind]):
            rows = table[kind][rate]
            dmg = [r['damaged_pct'] for r in rows]
            inc = [r['incomplete_pct'] for r in rows]
            comp = statistics.mean(r['completed_rps'] for r in rows)
            print(f"   {rate:7} {comp:9.0f} {sum(r['traces'] for r in rows):6} "
                  f"{statistics.mean(inc):11.1f}% {statistics.mean(dmg):9.1f}%")
            out.setdefault(kind, []).append({'offered_rps': rate, 'completed_rps': comp,
                                             'repetitions': len(rows),
                                             'incomplete_pct': statistics.mean(inc),
                                             'damaged_pct': statistics.mean(dmg),
                                             'damaged_pct_runs': dmg})
        print()
    if args.json:
        args.json.write_text(json.dumps({'reference_points': refs, 'series': out}, indent=1))


if __name__ == '__main__':
    main()
