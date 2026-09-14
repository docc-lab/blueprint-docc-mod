# SDK checkpoint refusal and reverse context

Tomislav-RetCtx: checkpoint rejection and upstream emission are decided by the
SDK on live spans. Rejection changes checkpoint data and priority; the span
still enters its normal export buffer. Application methods remain unchanged.

## Checkpoint and return decisions

With `REVERSE_TRUSS=on`, `RT_LEAF_REJECT` is the probability of rejecting an
**unscheduled server leaf**. Set it to `1` to reject every eligible leaf, or `0`
to retain leaf checkpoints. Invalid/nonpositive values act as zero; values above
one are clamped. Original checkpoints selected at `OnStart` always remain
checkpoints, including leaves whose incoming forward TTL was zero.

With the default `reverse_policy: ttl`, each rejected leaf independently draws
a **new reverse distance** from the
collector's inclusive `cpd_min`/`cpd_max` range. It attaches `distance - 1` as its
truss's reverse TTL. With legacy fixed `cpd`, the reverse distance is fixed too
(bounded to 1–256 for the byte TTL). The draw neither reuses the forward TTL nor
resizes the already constructed truss/Bloom filter.

At each receiving client **and** server span:

- An original forward checkpoint emits all returned trusses alongside its own
  checkpoint data, whatever their reverse TTLs.
- An ordinary span emits the trusses arriving with TTL zero in one checkpoint
  span, alongside its own data. It decrements and forwards the others.
- Becoming a checkpoint because of reverse TTL expiry does **not** consume
  nonexpired siblings or reset the forward window.

For example, an ordinary receiver with truss TTLs `[0, 2, 0]` emits the first
and third trusses, forwarding the second with TTL `1`. The next ordinary span
forwards it with TTL `0`; the following span emits it. An original checkpoint
encountered earlier terminates that return immediately. Distance 1 therefore
emits at the immediate parent; distance 256 permits 256 upstream span hops.

The actual trace root consumes remaining returns. `RT_ROOT=on` additionally
marks a process's server boundary as terminal; it does not turn every client
span in that process into a terminal receiver. Vanilla spans are all original
checkpoints: they retain their data and consume returns without leaf rejection.
SB's explicit synthetic `__bag.force_lp` spans remain LP and pass returns
unchanged; they do not spend a reverse hop.

Tomislav-RetCtx: the former provider-wide `RT_POLICY`/`RT_DEPTH` bundle decision
is removed from SDK routing. Those environment variables no longer select
emission points. Deployment helpers accept their old arguments for compatibility
and label them deprecated; CPD configuration determines reverse TTL distances.

Tomislav-RetCtx: `reverse_policy` also accepts `probability` (fixed configured p),
`inverse_depth` (1/origin depth), or `depth_linear` (higher probability at deeper
receivers). These modes independently sample each TTL-free returned truss;
original checkpoints still absorb all returns. See the
[probability formulas and configuration](reverse_checkpoint_probability.md).

## SDK and wrapper handoff

After the RPC returns, the client wrapper attaches `retCtx` to the recording span
as `__bag.rev_in` and calls `backend.PrepareCheckpoint`. It reads the SDK's
unconsumed output from `__bag.rev` into the request's fan-in accumulator. After
all calls complete and child/event counts are set, the server wrapper performs
the same handshake and returns the remaining output in its RPC response.

Preparation runs before `Span.End`, because the OTel SDK freezes span attributes
before calling the processor's `OnEnd`. Preparation is idempotent; `OnEnd` exports
the final classification. Receiving trusses are stored in `bridges.checkpoint`.
An emitting bridge span carries its own `_br` plus that returned-truss envelope.
Rejected leaves export ordinary `_d`/`_o` metadata while returning their `_br`.

Wrappers only transport and merge: they contain no checkpoint policy. If a span
is non-recording or the preparation hook is unavailable, received context passes
through unchanged. SDK decision attributes under `__bag.rev_` and reverse
carriers are excluded from forward baggage and exported span attributes.

## Return format and fan-in

The RPC still returns a string containing base64-encoded JSON with the existing
`fp`, `parent`, `m`, `k`, and `segs` envelope. Each checkpoint segment has:

```text
k:   checkpoint.pb | checkpoint.cgpb | checkpoint.sb | checkpoint.v
d:   origin span ID (8 bytes) || uvarint(absolute depth) || exact SDK truss bytes
ttl: independent reverse countdown (JSON integer, 0–255)
```

Tomislav-RetCtx: `ttl` is separate segment metadata, leaving the binary `d`
payload unchanged. Probability-mode checkpoints also omit the TTL. A TTL-free
checkpoint uses the receiver's configured probability policy, or waits for an
original checkpoint/root under the default TTL policy. Explicit TTL segments
retain their countdown in mixed bundles. Use consistent SDK configurations when
comparing policies; readers can still decode older segments.

`backend.DecodeReturnedCheckpoints` returns the origin, depth, exact truss and
optional `ReverseTTL`. Depth counts client and server spans from zero at the
root and never changes in transit. PB/CGPB use their native absolute depth;
SB additionally propagates `__rt_depth` because its truss depth is window-relative.

`MergeRetCtx` and the mutex-protected request accumulator append sibling and
nested descendant segments. Fan-in spends no TTL hop and never combines filters
or replaces origins. Arrival order determines sibling order. Each ranged PB/CGPB
truss retains its immutable distance descriptor and its own Bloom size; envelope
`m`/`k` describe only legacy ancestry AMQs. SB's delayed-end-event queue continues
at `OnEnd`. See [forward TTL and geometry](randomized_checkpoint_distance.md).

## Verification and rollout

```bash
source ~/.profile
source .venv/bin/activate
go test -race ./runtime/core/backend ./runtime/plugins/otelcol ./plugins/opentelemetry
```

Tests cover original checkpoint protection, independent draws, TTL 0/255,
partial emission, repeated preparation, mixed legacy returns, exact origin/truss
preservation, nested concurrent fanout, and generated wrappers. `RTCTX='<retctx>'
go test ./runtime/core/backend -run TestRTVerify -v` inspects return envelopes.

Rebuild application images with this SDK before enabling the new policies.
Collector CPD bounds supply the default reverse TTL range; `reverse_policy` and,
for fixed probability, `reverse_probability` select the alternatives. Rebuilding
the collector includes validation of the new keys. This revision is locally
tested and not deployed; the earlier Social Network measurements used rejection
disabled. See the [session change map](tomislav_retctx_changes.md).
