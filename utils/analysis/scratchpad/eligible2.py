#!/usr/bin/env python3
"""Tomislav-RetCtx: reconstruction eligibility against the TRUE checkpoint set.

The checkpoints of a trace are (a) the scheduled window roots and (b) the 8 leaves
of the ComposePost tree. A leaf checkpoints either locally (its own record carries
`_br`) or, with the response path on, by returning its truss to an upstream carrier
whose `_rc` names the leaf's span id; the carrier is transport, the checkpoint is
the leaf's. A trace is eligible iff every window root named by a surviving payload
is present (or `_rc`-recovered) AND every leaf's truss is present somewhere.
Leaf positions are known from the topology, so an absent leaf with no `_rc` naming
it is a LOST checkpoint. An absent interior position with no surviving descendant
cannot be classified from the wire (its scheduled status is unknowable) and is
counted separately as undetermined.
"""
import base64, collections, datetime, glob, gzip, json, sys

def uv(b, i=0):
    r = s = 0
    while True:
        c = b[i]; i += 1; r |= (c & 0x7f) << s
        if c < 0x80: return r, i
        s += 7

def tag(sp, k): return next((t for t in sp['tags'] if t['key'] == k), None)

def canonical(traces):
    for t in traces:
        if len(t['spans']) != 23: continue
        by = {s['spanID']: s for s in t['spans']}; parent = {}
        for s in t['spans']:
            p = [r['spanID'] for r in s['references'] if r['refType'] == 'CHILD_OF']
            parent[s['operationName']] = by[p[0]]['operationName'] if p and p[0] in by else ''
        if len(parent) == 23: return parent
    return None

def analyse(traces, parent):
    leaves = {op for op in parent if op not in set(parent.values())}
    depth = {}
    def d(op): return 0 if parent[op] == '' else 1 + d(parent[op])
    for op in parent: depth[op] = d(op)
    def ancestors(op):
        out = []; p = parent[op]
        while p: out.append(p); p = parent[p]
        return out
    stats = collections.Counter(); per = []
    for t in traces:
        present = {s['operationName']: s for s in t['spans']}
        ids = {s['spanID'] for s in t['spans']}
        anchors = set(); rc = []  # (carrier_op, origin_id, origin_depth)
        for s in t['spans']:
            br = tag(s, '_br')
            if br:
                raw = base64.b64decode(br['value']); _, i = uv(raw); a = raw[i:i + 8]
                if any(a): anchors.add(a.hex())
            r = tag(s, '_rc')
            if r:
                v = r['value']; raw = base64.urlsafe_b64decode(v + '=' * (-len(v) % 4)); i = 1
                while i < len(raw):
                    tg = raw[i]; i += 1; ln, i = uv(raw, i); body = raw[i:i + ln]; i += ln
                    j = 1 if tg & 0x08 else 0
                    oid = body[j:j + 8].hex(); od, _ = uv(body, j + 8)
                    rc.append((s['operationName'], oid, od))
                    tr = body[j + 8 + (uv(body, j + 8)[1] - (j + 8)):]
                    if len(tr) >= 9:
                        _, k = uv(tr); a = tr[k:k + 8]
                        if any(a): anchors.add(a.hex())
        rc_ids = {o for _, o, _ in rc}
        anchors_ok = not (anchors - ids - rc_ids)
        # leaves: present with _br -> ok; present as LP -> needs an _rc naming its id;
        # absent -> needs an unmatched _rc origin at its depth from an ancestor carrier
        unmatched = collections.defaultdict(list)  # (carrier_op, depth) -> [origin ids]
        for c, o, od in rc:
            if o not in ids: unmatched[(c, od)].append(o)
        lost_leaves = 0
        for L in sorted(leaves):
            s = present.get(L)
            if s is not None:
                if tag(s, '_br'): continue
                if s['spanID'] in rc_ids: continue
                lost_leaves += 1; continue
            # absent leaf: find an ancestor carrier with a spare origin at this depth
            ok = False
            for c in ancestors(L):
                q = unmatched.get((c, depth[L]))
                if q: q.pop(); ok = True; break
            if not ok: lost_leaves += 1
        # undetermined: absent interior position with no surviving descendant
        undet = 0
        for op in parent:
            if op in present or op in leaves: continue
            if not any(op in ancestors(x) for x in present): undet += 1
        eligible = anchors_ok and lost_leaves == 0
        stats['n'] += 1; stats['eligible'] += eligible; stats['anchor_fail'] += (not anchors_ok)
        stats['leaf_fail'] += (lost_leaves > 0); stats['undetermined'] += (undet > 0)
        stats['intact'] += (len(t['spans']) == 23)
        per.append((t, eligible))
    return stats, per

def main(paths):
    can = None
    for label, gz in paths:
        traces = json.load(gzip.open(gz))['data']
        can = can or canonical(traces)
    if not can:
        print('no intact trace to learn topology'); return
    print(f"{'sample':40} {'n':>4} {'eligible':>9} {'intact':>7} {'anchor-fail':>11} {'leaf-ckpt-lost':>14} {'undetermined':>12}")
    for label, gz in paths:
        traces = json.load(gzip.open(gz))['data']
        st, _ = analyse(traces, can)
        n = st['n']
        print(f"{label:40} {n:4} {100*st['eligible']/n:8.0f}% {100*st['intact']/n:6.0f}% {100*st['anchor_fail']/n:10.0f}% {100*st['leaf_fail']/n:13.0f}% {100*st['undetermined']/n:11.0f}%")

if __name__ == '__main__':
    args = sys.argv[1:]
    main([(args[i], args[i + 1]) for i in range(0, len(args), 2)])
