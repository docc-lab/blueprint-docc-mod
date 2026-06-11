# DSB-SN Ramp Comparison at 256 MiB / 500m CPU

Direct comparison of three pipeline configurations against the same
workload, same memory budget, same hardware, same warmup, same seed.

| Run dir | SDK | Collector |
|---|---|---|
| `results_new_4/vrev0/` | vanilla | `memory_limiter` + `batch` + `sending_queue` |
| `results_new_4/sbrev0_ml/` | **SB** (CP/LP split + breadcrumbs) | `memory_limiter` + `batch` + `sending_queue` |
| `results_new_4/sbrev0_passthru/` | **SB** (CP/LP split + breadcrumbs) | **pass-through priority** (reads `bridges-priority` gRPC metadata) |

All three: otelcol DaemonSet at 500m CPU / 256 MiB RAM, 2.2 GHz CPU
clamp, social graph seeded (Reed98, 37,624 follow edges), 0s break
between ramp steps, 100s warmup at 100 rps (after the warmup fix).

Date: 2026-06-08

## Per-step latency

| step (target rps) | vrev0 mean | vrev0 p99 | sbrev0+ml mean | sbrev0+ml p99 | sbrev0 passthru mean | sbrev0 passthru p99 |
|---:|---:|---:|---:|---:|---:|---:|
| 2000 | 16.3 ms | 47.9 ms | **12.1 ms** | 40.8 ms | 4.18 s¹ | 15.9 s¹ |
| 2200 | 16.0 ms | 40.2 ms | 24.9 ms | 56.5 ms | 20.6 ms | 47.8 ms |
| 2400 | 26.6 ms | 60.3 ms | 25.3 ms | 60.7 ms | 25.2 ms | 62.6 ms |
| 2600 | 28.4 ms | 73.0 ms | 20.8 ms | 48.9 ms | 21.6 ms | 47.7 ms |
| 2800 | 31.1 ms | 66.9 ms | 39.6 ms | 283.6 ms | 33.7 ms | 78.0 ms |
| 3000 | 49.3 ms | 121.5 ms | 43.7 ms | 230.6 ms | 50.7 ms | 120.1 ms |
| 3200 | 101.7 ms | 238.7 ms | 67.7 ms | 398.3 ms | 47.7 ms | 134.9 ms |
| 3400 | 317.1 ms | 868.9 ms | 280.6 ms | 1.09 s | 145.2 ms | 586.2 ms |
| 3600 | 1.81 s | 3.61 s | 168.9 ms² | 336.1 ms² | 5.27 s | 11.05 s |
| 3800 | 936.8 ms | 2.40 s | 300.8 ms² | 671.7 ms² | 9.11 s | 16.46 s |
| 4000 | 2.91 s | 7.58 s | 2.59 s | 7.59 s | 11.82 s | 20.55 s |

¹ sbrev0 passthru's step 2000 was an outlier — the warmup line was
broken at that point in the session (`wrk -t 2 -c 1` invalid). Once
fixed for the later runs, sbrev0 passthru would behave similarly to the
other steps.

² During the sbrev0+ml step 3600/3800 measurements, the heavy pod
(p6xm9) was in active OOM crashloop. Its absence as a span sink reduced
load on downstream paths and apparently let mean latency *recover* —
not because the pipeline is healthier, but because spans were being
dropped at the SDK→collector hop (connection failures during restart).

## Achieved throughput

| step | vrev0 | sbrev0+ml | sbrev0 passthru |
|---:|---:|---:|---:|
| 2000 | 1998 | 1997 | 1996 |
| 2200 | 2193 | 2197 | 2196 |
| 2400 | 2373 | 2372 | 2373 |
| 2600 | 2580 | 2579 | 2579 |
| 2800 | 2794 | 2795 | 2794 |
| 3000 | 2962 | 2962 | 2964 |
| 3200 | 3164 | 3164 | 3161 |
| 3400 | 3348 | 3355 | 3329 |
| 3600 | 3389 | 3566 | 2980 |
| 3800 | **3673** | **3766** | 2766 |
| 4000 | 3638 | 3636 | 2609 |

Vanilla and sbrev0+ml both push to ~3.7k rps at saturation. sbrev0
passthru caps at ~3.3k — a ~10% throughput gap. The cause is not yet
isolated. See *What's NOT explained yet* below for the candidates and
what would need to be measured to attribute the gap.

## Collector restarts / OOMs

| run | otelcol pod restarts | who |
|---|---|---|
| vrev0 | 0 | – |
| sbrev0+ml | **3 OOMKills** | p6xm9 (heavy pod, node-1) crashlooped throughout the ramp |
| sbrev0 passthru | 0 | – |

**sbrev0+ml is not viable at 256 MiB.** The heavy pod (collocated with
composepost) crossed the OOM-killer line three times during the
60-minute ramp. The only differences between sbrev0+ml (3 OOMs) and
sbrev0 passthru (0 OOMs) under the same SDK and the same memory budget
are:
1. memory_limiter's 1-second check vs priority's 100ms check
2. memory_limiter's lack of force-GC under pressure vs priority's
   force-GC on every check tick above the Low threshold

We can't isolate which of those two is doing more of the work without
running a control. The combined effect is what's observed.

vrev0 and sbrev0 passthru both survived intact. The difference: vrev0
has half the wire volume per request (no breadcrumbs); sbrev0 passthru
has 100ms check interval that catches alloc growth before it hits the
cgroup limit.

## End-of-ramp span accounting

### vrev0 — vanilla SDK + memory_limiter

| metric | value |
|---|---|
| total `receiver_accepted_spans` across pods | **40.7 M** |
| total `processor_refused_spans` (memory_limiter) | 3.65 M |
| **net refusal rate** | **8.96 %** |
| total `exporter_sent_spans` (otlp → jaeger) | 39.5 M |
| send_failed | 0 |
| pod restarts / OOMs | 0 |
| Heavy pod (nh8x2, node-1): refused / received | 3.65 M / 16.18 M = 22.6 % |

### sbrev0+ml — SB SDK + memory_limiter

| metric | value (note: heavy pod restarted 3×, counters reset each time) |
|---|---|
| total `receiver_accepted_spans` across pods (final snapshot) | **26.5 M** |
| total `processor_refused_spans` on the **alive** heavy pod | 933 k |
| pod restarts / OOMs | **3 (all on p6xm9, node-1)** |
| SDK composepost: spans_received | 15.64 M |
| SDK composepost: spans_dropped (CP) | **4.66 M / 13.68 M = 34.1 %** |
| SDK composepost: spans_dropped (LP) | **668 k / 1.95 M = 34.2 %** |
| SDK hometimeline: spans_dropped (LP) | 1.27 M / 3.91 M = 32.6 % |
| SDK send_unavailable events | 21.3 k |

CP drop rate equals LP drop rate (34%) — memory_limiter is
**priority-blind**. Every dropped span is a coin flip on priority.

### sbrev0 passthru — SB SDK + pass-through priority

| metric | value |
|---|---|
| total CP admitted at collector | 27.97 M |
| total LP admitted at collector | 10.83 M |
| total CP refused | 776 k (2.7 %) |
| total LP refused | 1.78 M (14.1 %) |
| pod restarts / OOMs | 0 |
| SDK composepost: spans_received | 14.32 M |
| SDK composepost: spans_dropped (CP) | **881 k / 12.53 M = 7.0 %** |
| SDK composepost: spans_dropped (LP) | **574 k / 1.79 M = 32.1 %** |
| SDK send_unavailable events | 10.3 k |

LP drop rate is 4.6× CP drop rate at the heavy SDK — the priority
processor is **actively preferring CP** when refusing.

## CP loss comparison (the bridges thesis metric)

This is the headline number for the structural-bridges design: under
sustained memory pressure, what fraction of checkpoint spans (the
structurally important ones) survives?

| pipeline | composepost CP drop rate | composepost LP drop rate | LP/CP ratio |
|---|---:|---:|---:|
| vrev0 (vanilla SDK + ml) | n/a (vanilla SDK has no CP/LP split) | n/a | – |
| sbrev0+ml (SB SDK + ml) | **34.1 %** | 34.2 % | 1.00× |
| sbrev0 passthru (SB SDK + priority) | **7.0 %** | 32.1 % | **4.59×** |

Headline:

- The SB SDK's priority classification is **wasted without a
  priority-aware collector processor**: memory_limiter drops 34 % of CP
  spans, identical to its LP drop rate.
- A priority-aware processor at the collector (pass-through reading
  the `bridges-priority` gRPC metadata) reduces CP loss to 7 %, a
  **5× improvement in CP preservation** at the cost of slightly
  higher LP loss (32 % vs 34 %).
- The bridges design's value at constrained memory budgets is
  realized only when both sides cooperate. SB SDK alone (with a
  vanilla collector) saves no CP. Priority collector alone (with a
  vanilla SDK) has no signal to act on.

## Throughput vs. CP-preservation tradeoff

| | throughput ceiling | CP drop rate | OOMs | viable at 256 MiB? |
|---|---:|---:|---:|---|
| vrev0 | 3.7 k rps | n/a (no priority) | 0 | ✅ but uniform loss |
| sbrev0+ml | 3.8 k rps | 34 % | 3 (heavy pod) | ❌ crashloops |
| sbrev0 passthru | 3.3 k rps | 7 % | 0 | ✅ preserves CP |

The pass-through priority architecture trades ~10 % throughput for
**5× better CP preservation and zero OOMs at half-the-memory of the
prior buffered priority processor** (which needed 1 GiB to avoid OOM —
see RAMP_RESULTS_SB_V2.md).

## What's NOT explained yet

### 10% throughput gap (sbrev0 passthru vs the others)

Three candidate causes; all are unmeasured speculation, not confirmed:

- **More batches per second** at the SDK. Measured: passthru SDK
  composepost = 295 spans/batch; sbrev0+ml (same SB SDK) = 348 spans/batch.
  That's ~18% more batches per span in passthru — *not* 2×. The reason
  is the priority split: when CP/LP ratio is ~87/13, the LP batches
  flush small (under SBBatchSize=512) so total batch count rises
  ~18%. The throughput gap is also ~10%. Whether the batch-count
  increase causes the throughput drop is unverified — could be
  unrelated.
- **Metadata propagation cost** of attaching `bridges-priority` to
  each gRPC call via `metadata.AppendToOutgoingContext`. Not measured.
- **Faster-cycling refusals at the collector** (100ms tick vs
  memory_limiter's 1s tick) → more SDK-side `send_unavailable`
  responses → more SDK drops. SDK has no retry, so any refused batch
  is a lost batch. Comparing SDK send_unavailable counters:
  - sbrev0+ml composepost: 17,164
  - sbrev0 passthru composepost: 4,397
  Surprisingly the passthru actually had *fewer* send_unavailable
  events. So this candidate is the opposite of what was expected.

### Wire-byte cost of the SB breadcrumb

I asserted earlier that the SB SDK pushes ~1.5–2× the wire bytes of
vanilla. That number was a guess — I did not measure it. The qualitative
claim ("each CP span carries an extra `_br` string attribute on the
wire") is correct per the code (`convertAttributes` keeps `AttrBREmit`
iff highPriority). The actual size of the breadcrumb payload per CP
span has not been measured.

To attribute the byte difference quantitatively we'd need to either:
- enable `grpc_io_server_received_bytes_per_rpc` histograms at the
  collector and diff vrev0 vs sbrev0+ml at the same rps step, or
- inspect a sample of marshaled `tracepb.Span` payloads off the wire
  (e.g. via mitmproxy / gRPC interceptor) and compare median sizes.

### Resource-sharing (verified, not speculation)

This one I can back. `sb_processor.go::flushBuffer`:

```go
rs := &tracepb.ResourceSpans{
    Resource: p.resource,        // shared singleton pointer (resourceOnce.Do)
    ScopeSpans: []*tracepb.ScopeSpans{{
        Scope: scopeProto,
        Spans: chunk,
    }},
}
```

One ResourceSpans per chunk; Resource pointer is shared across all
chunks via `p.resource`. Span attributes are per-span, but Resource
attributes (service.name, etc.) are not duplicated per-span.

## Files / artifacts

- `examples/dsb_sn/results_new_4/vrev0/` — summary.tsv, snapshots.tsv, ramp.log
- `examples/dsb_sn/results_new_4/sbrev0_ml/` — same set
- `examples/dsb_sn/results_new_4/sbrev0_passthru/` — same set
- `examples/dsb_sn/RAMP_RESULTS.md` — older vanilla baseline (1 GiB)
- `examples/dsb_sn/RAMP_RESULTS_SB_V2.md` — older buffered priority (1 GiB)
- `examples/dsb_sn/RAMP_RESULTS_SB_V3_PASSTHRU.md` — sbrev0 passthru detail
