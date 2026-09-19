# Reverse checkpoint probability modes: simulator primer

Tomislav-RetCtx: handoff for simulator work, September 14, 2026. The implemented
behavior below was checked against Blueprint commit `6997134e` on
`retctx-reject-impl` and collector configuration commit `ca8f540bd7`. The
increasing-upstream-pressure mode is a proposal, not an implemented SDK mode.
All formulas use plain-text notation.

## 1. What the policies control

A bridge span can be an **original checkpoint**, selected by the forward
checkpoint-distance mechanism at span start. A server leaf that was not selected
normally becomes an additional checkpoint so its unfinished truss is retained.
With rejection enabled, that unscheduled leaf can instead return its truss
upstream. The reverse policy decides which receiving span will emit it.

Checkpoint rejection does not reject the span from the SDK export buffer. The
leaf still exports ordinary span metadata; its checkpoint payload travels in the
RPC response. A receiver that accepts returned trusses emits them in its own
checkpoint span, alongside its own checkpoint data. Later collector dropping is
a separate decision and should be modeled separately.

There are three independent settings:

| Setting | Meaning |
| --- | --- |
| Forward CPD | Where original checkpoints are scheduled. |
| Leaf rejection probability `q` | Whether an eligible unscheduled server leaf returns its truss. SDK setting: `RT_LEAF_REJECT`. |
| Reverse acceptance probability `p` | Whether a receiving span emits one particular returned truss. Determined by `reverse_policy`. |

Use `REVERSE_TRUSS=on` and `q=1` to exercise every eligible leaf. Original
checkpoints, roots, and non-leaves do not reject. Vanilla spans are all original
checkpoints, so these modes principally concern PB, CGPB, and SB.

## 2. Depth and immutable state

For one returned truss:

- `n` is its originating leaf's absolute span depth, with root depth zero.
- `d` is the current receiving span's absolute depth; a valid ancestor has
  `0 <= d < n`.
- `c` is the nearest original checkpoint upstream, used in analytical examples.
- Preserve the origin span ID, `n`, bridge kind, and exact truss bytes throughout
  the return path. Receiving or merging the truss never rewrites them.

`n` stays constant. It is not remaining depth, CPD, the deepest sibling, or the
maximum depth anywhere in the trace. Each sibling truss has its own `n`.

The SDK counts **both client and server spans**. A service-to-service RPC can
therefore introduce two receiver decisions on its return path: at the caller's
client span and then its enclosing server span. Declare the simulator's depth
unit. For SDK parity, represent these spans explicitly; one decision per service
hop gives a different policy. A shared service in a deployment DAG still creates
distinct span instances on separate calls, each with its own ancestor path.

Keep an immutable `original_checkpoint` flag from the forward pass. A span that
becomes a checkpoint because it accepts a return does not gain that flag.

## 3. Modes and their status

These probabilities apply to an ordinary receiver that the truss actually reaches:

| Mode | Probability `p(d,n)` | Status |
| --- | --- | --- |
| `probability` | Configured constant `p0`, between 0 and 1 inclusive | Implemented; requires `reverse_probability: p0` |
| `inverse_depth` | `1 / n` | Implemented |
| `depth_linear` | `2 * (d + 1) / (n * (n + 1))` | Implemented |
| Increasing upstream pressure | `1 / (d + 1)` | Proposed; no SDK configuration name yet |

`ttl` is the implemented default and the non-probabilistic comparison: each
rejected truss independently draws a reverse distance `D`, starts with TTL
`D - 1`, and is emitted at a receiver that sees zero. Otherwise that receiver
decrements and forwards it. An original checkpoint terminates it sooner.

**Fixed probability.** Each receiver performs a fresh independent Bernoulli
trial. With `p0=0`, returns wait for the original checkpoint/root. With `p0=1`,
they are accepted by the first eligible receiving span. This is a useful control.

**Constant `1/n`.** Deeper-origin trusses have a lower acceptance probability,
but their probability does not change as they move upstream. Although the
formula is depth-based, its input is origin depth, not receiver depth.

**Depth-linear.** The weights `d+1` sum to `n*(n+1)/2` over possible receiver
depths `0..n-1`; the formula divides by that sum. A deeper receiver therefore has
a higher conditional acceptance probability. This normalizes the sum of the
per-node probabilities to one, as for `1/n`. It does not normalize the final
emission distribution or guarantee equal checkpoint counts or byte costs.

**Proposed increasing pressure.** As a truss moves upward, `d` decreases and
`1/(d+1)` increases. For a leaf at depth six, the probabilities are `1/6`, `1/5`,
`1/4`, `1/3`, `1/2`, then root absorption. On a simple chain with no intervening
checkpoint, this gives equal final emission probability at each of the six
receivers. With a checkpoint at depth `c`, each earlier ordinary receiver still
gets probability `1/n`, and the checkpoint absorbs the remaining `(c+1)/n`.
These properties assume consecutive, eligible receiver depths.

Increasing the conditional probability upstream is different from merely having
an increasing cumulative chance of acceptance, which already happens under a
constant nonzero probability. The proposed mode also need not increase average
return distance relative to constant `1/n`: it accepts survivors more strongly
before they accumulate at the root.

## 4. Mandatory absorption and worked examples

An original checkpoint or the actual root emits **all** arriving trusses,
irrespective of their probability or TTL. No random draw is needed there. Treat
the probability formulas as conditional trials before that boundary.

For a leaf at `n=6`, the conditional probabilities are:

| Receiver depth | Fixed `p0=0.25` | `inverse_depth` | `depth_linear` | Proposed increasing pressure |
| --- | ---: | ---: | ---: | ---: |
| 5 | 25.00% | 16.67% | 28.57% | 16.67% |
| 3 | 25.00% | 16.67% | 19.05% | 25.00% |
| 1 | 25.00% | 16.67% | 9.52% | 50.00% |
| Original checkpoint/root | 100% | 100% | 100% | 100% |

To obtain final emission probabilities, carry the probability of surviving all
earlier receivers:

```text
survival = 1
for d = n-1 down to c+1:
    emission_probability[d] = survival * p(d,n)
    survival = survival * (1 - p(d,n))
emission_probability[c] = survival
```

For `n=6` and only the root as an original checkpoint (`c=0`), this gives:

| Emission depth | Fixed `p0=0.25` | `inverse_depth` | `depth_linear` | Proposed increasing pressure |
| --- | ---: | ---: | ---: | ---: |
| 5 | 25.00% | 16.67% | 28.57% | 16.67% |
| 4 | 18.75% | 13.89% | 17.01% | 16.67% |
| 3 | 14.06% | 11.57% | 10.37% | 16.67% |
| 2 | 10.55% | 9.65% | 6.29% | 16.67% |
| 1 | 7.91% | 8.04% | 3.60% | 16.67% |
| 0: root absorption | 23.73% | 40.19% | 34.17% | 16.67% |
| Mean return distance, in span hops | 3.288 | 3.991 | 3.418 | 3.500 |

Percentages are rounded. Mean distance is the sum of
`(n - emission_depth) * emission_probability`. These are analytical expectations,
not measured benchmark results. In particular, constant `1/n` does not choose
uniformly among emission locations; about 40% reach the root in this example.

If the nearest original checkpoint is instead at depth two, depths five, four,
and three retain the same probabilities. The checkpoint at depth two absorbs
42.19%, 57.87%, 44.06%, and 50.00%, respectively. None reaches depths one or zero.
Do not renormalize the formulas around this nearer checkpoint: the implemented
depth-based policies still use the original absolute `n` and `d`.

## 5. Receiver algorithm and fan-in

Apply this once at each span's completion, after its child returns have arrived:

```text
route_receiver(span, pending_segments):
    if span is non-recording or an SB synthetic forced-LP span:
        return emitted=[], forwarded=pending_segments

    if span.original_checkpoint or span.is_return_boundary:
        return emitted=pending_segments, forwarded=[]

    emitted, forwarded = [], []
    for segment in pending_segments:
        if segment has an explicit reverse TTL:
            if TTL == 0: append segment to emitted
            else: decrement TTL; append segment to forwarded
            continue

        if segment is not a valid checkpoint truss or not 0 <= d < segment.n:
            append segment to forwarded
            continue

        p = probability_for_selected_mode(d, segment.n)
        if p >= 1 or (p > 0 and independent_uniform_0_to_1() < p):
            append segment to emitted
        else:
            append segment to forwarded

    return emitted, forwarded
```

In default TTL mode, a segment without a TTL waits for an original checkpoint;
there is no probability trial. Explicit TTL segments keep their countdown even
in a bundle handled by a probability-configured receiver. Opaque or malformed
TTL-free context passes through ordinary receivers and is absorbed at the
mandatory boundary. The SDK's optional `RT_ROOT=on` also makes that process's
server boundary terminal; model this override only when the deployment uses it.

Merge fan-in by appending pending segments from siblings and deeper descendants.
Merging itself performs no probability trial or TTL decrement. The enclosing
receiving span subsequently performs its own decision. Do not merge Bloom
filters, replace sibling origins, or use the deepest sibling's `n` for the bundle.

For example, two trusses with `n=6` and `n=10` meet at an ordinary receiver at
`d=3`. Under `inverse_depth`, their probabilities are `1/6` and `1/10`. Test draws
of `0.12` for each would emit the first and forward the second. Accepting the
first does not make the receiver an original checkpoint that also absorbs the
second. Use independent draws in normal simulation; these fixed draws just make
the example deterministic.

All accepted trusses at a receiver share **one emitted checkpoint span**, along
with that span's own data. Unaccepted siblings continue upstream. Accepting a
return neither resets the forward checkpoint window nor reseeds descendant CPD.
Repeated preparation of a span must not cause another trial. The SDK performs
this preparation while the span is recording, before `End` freezes its attributes;
export follows at `OnEnd`.

## 6. Forward state and simulation reproducibility

Keep forward scheduling identical when comparing reverse modes. A root or
scheduled checkpoint draws one inclusive integer distance from `cpd_min..cpd_max`
and sends TTL `distance-1`; a child receiving zero is an original checkpoint,
and a child receiving nonzero decrements its own copy. Sibling paths initially
share their parent's selected window, then draw independently when they next
reach original checkpoints.

Each PB/CGPB Bloom is sized for its selected window, not the range maximum.
Returned trusses retain that window descriptor and exact filter bytes. A reverse
acceptance decision must not resize or regenerate an incoming truss. SB's
structural encoding remains its own responsibility; the routing rule is shared.

For simulator comparisons, use separate reproducible random streams for forward
CPD, leaf rejection, reverse TTL, and reverse probability. Keying a reverse trial
by seed, trace, origin span, and receiver span makes results insensitive to the
order concurrent sibling returns are processed. This is a recommended simulation
method, not a claim of bit-for-bit equivalence with the SDK's concurrent Go RNG.
Use matched workloads and report observed CPD distributions.

## 7. Payloads and what to measure

A probability-mode segment contains:

```text
kind: checkpoint.pb | checkpoint.cgpb | checkpoint.sb
data: origin_span_id(8 bytes) || unsigned_varint(n) || exact_truss_bytes
reverse TTL: absent
```

The origin metadata already supplies `n`; no new probability field is carried.
The receiving SDK uses its configured policy, so use consistent policies along
the path. In the current wire representation, the binary segment is serialized
as a base64 JSON string in `d`, inside a JSON envelope that is itself base64-encoded
into the response's `retCtx` string. The envelope also carries fingerprints and
legacy metadata. Returned trusses accepted for export appear under
`bridges.checkpoint`; the receiver retains its own `_br`.

Report these quantities separately:

- Original checkpoints, forced leaf checkpoints, rejected leaves, distinct
  receiving checkpoint spans, and number of returned trusses each carries.
- Emission depth, reverse distance in span hops, and mandatory-boundary absorption
  fraction, stratified by origin depth and nearest original checkpoint.
- Raw truss bytes, origin/segment metadata, and complete exported checkpoint
  payload bytes, including the receiver's own data.
- Encoded `retCtx` bytes on **every RPC response hop**. A client-span decision
  and its enclosing server-span decision are not two separate network responses.
  Distinguish logical span hops from physical RPC byte traffic.
- Under loss, retained structure/reconstruction quality against ground truth,
  separately from checkpoint routing correctness before loss.

Current `BRIDGES_RT` counters record events, not all these byte totals. The
existing `payload_percentages.json` histogram counts emitted bridge bytes with
the `_br` key/type overhead; it does not describe reverse RPC traffic or the new
`bridges.checkpoint` envelope. Generate clearly labeled distributions for those
quantities rather than folding them into the old metric without changing its
definition. Fewer checkpoint spans need not mean fewer bytes: an accepting span
can add its own checkpoint data, and a returned payload can cross several RPCs.

## 8. Configuration, checks, and source pointers

Implemented collector-discovery configuration for a probability comparison:

```yaml
config_map:
  cpd_min: 2
  cpd_max: 8
  reverse_policy: depth_linear
```

Use `inverse_depth` to select `1/n`. For fixed probability, set
`reverse_policy: probability` and `reverse_probability: 0.25`. Only that mode
accepts `reverse_probability`; the value must be finite and in `[0,1]`. The SDK
fetches configuration at startup. The proposed increasing-pressure mode is not
accepted by the current SDK, collector validator, or deployment scripts.

Minimum simulator checks: protect original checkpoints; test `p=0` and `p=1`;
match the analytical distributions above; preserve each truss's origin and bytes;
emit each rejected truss exactly once before modeled loss; keep sibling decisions
independent; distinguish original from reverse-created checkpoints; and preserve
the forward checkpoint schedule. Include unequal branch depths, concurrent
fanout, a shared-service DAG, and mixed explicit-TTL/probability bundles when
testing SDK compatibility.

The implemented modes passed SDK/backend/wrapper and collector-validator race
tests, including 79-span concurrent trees. Probability-mode deployment evaluation
is still pending. The ongoing standalone collector load tests use synthetic span
payload distributions; they do not exercise this reverse protocol end to end.

Implementation references:

- [Probability formulas and routing](../../runtime/plugins/otelcol/reverse_policy.go)
- [SDK eligibility and span lifecycle](../../runtime/plugins/otelcol/reverse_checkpoint.go)
- [Carrier, fan-in, and mixed-TTL routing](../../runtime/core/backend/reversetruss.go)
- [Policy and independent-trial tests](../../runtime/plugins/otelcol/reverse_policy_test.go)
- [Forward CPD and Bloom geometry](randomized_checkpoint_distance.md)
- [Existing probability configuration guide](reverse_checkpoint_probability.md)
- Collector validation: `opentelemetry-collector-contrib/receiver/configdiscoveryreceiver/reverse_policy.go`
