# Randomized checkpoint distance

Tomislav-RetCtx: PB, CGPB, and SB select a new checkpoint distance at each root and scheduled
checkpoint. Configure an inclusive range in the collector's existing SDK config
discovery receiver:

```yaml
receivers:
  configdiscovery:
    endpoint: ":8080"
    config_map:
      cpd_min: 2
      cpd_max: 6
```

Keep `configdiscovery` in the collector's existing logs/config pipeline. The SDK
fetches `/getFullConfig` at startup, as it does for fixed `cpd`. Both bounds are
required, and `1 <= cpd_min <= cpd_max <= 256`. Bounds take precedence over a
legacy `cpd` value if all three keys are supplied in YAML. The deployment CLI
requires choosing either `--cpd` or the two range flags.

## Span lifecycle

At `OnStart`, a root or a span receiving TTL zero selects an integer uniformly
from the inclusive range. It becomes a scheduled checkpoint and writes
`selected_distance - 1` into its outgoing baggage. An ordinary span receiving a
nonzero TTL decrements it once for its descendants.

For `cpd_min: 2, cpd_max: 2`, a path is:

| Span depth | Incoming TTL | Scheduled checkpoint | Outgoing TTL |
| --- | --- | --- | --- |
| 0 (root) | absent | yes | 1 |
| 1 | 1 | no | 0 |
| 2 | 0 | yes | 1 |
| 3 | 1 | no | 0 |
| 4 | 0 | yes | 1 |

Distances count instrumented spans, including client and server spans. A
distance of 1 checkpoints every span; a distance of 256 writes TTL 255.

Each checkpoint draws once for its outgoing paths. All siblings receive the
same parent TTL. Each decrements its own copy, including during concurrent
fanout. When sibling paths next reach checkpoints, those checkpoints draw
independently. Draws use Go's concurrency-safe random generator.

The SDK records the scheduled role at start and exports the checkpoint at
`OnEnd`. Tomislav-RetCtx: original checkpoints cannot reject. With reverse
rejection enabled, unscheduled leaves may return their truss with a fresh,
independently drawn reverse TTL from this same CPD range. An original checkpoint
absorbs all returns; an ordinary receiver emits only expired trusses and forwards
the rest. Neither countdown alters the original span ID, depth, or truss bytes.
The forward TTL is excluded from returned trusses; reverse TTL is separate
segment metadata. See [reverse routing](reverse_truss.md).

Tomislav-RetCtx: [probabilistic reverse policies](reverse_checkpoint_probability.md)
can replace that reverse TTL with independent per-truss acceptance trials.
Forward distance selection and each Bloom window's sizing remain the same.

## Encoding and bridge state

Range mode keeps the one-byte countdown at the front of decoded **forward
baggage**. PB and CGPB also store an immutable window distance beside the Bloom
filter, so its geometry can be recovered after the TTL has been decremented:

```text
PB/CGPB truss = uvarint(absolute_depth) || checkpoint_span_id(8)
               || byte(window_distance - 1) || bloom_bytes || optional_CGPB_HA
forward _br = base64url_no_padding(ttl_byte || truss)
```

The TTL prefixes the complete payload, before its depth varint. The
instrumentation wrappers transport it through `__bag._br` without parsing it.
Exported PB/CGPB `_br` trusses retain the window-distance descriptor but omit
the mutable TTL. The existing `_d`/`_o` attributes and SB truss format are
unchanged; SB has no path Bloom filter to size.

PB and CGPB preserve absolute depth and reset their checkpoint anchor and
window state when TTL expires. Each new filter is sized for the **selected
distance minus one** intervening spans, with minimum capacity 1 and false
positive target 0.0001. The receiver derives the exact Bloom m/k and byte width
from the immutable distance byte. That width also locates CGPB's trailing HA.
For distances 2, 3, 4, 5, and 6, the filters occupy 3, 5, 8, 10, and 12 bytes.

An ordinary span preserves its window's distance and filter geometry while
decrementing the TTL. At a checkpoint, the emitted/returned truss keeps the
incoming window's geometry; the outgoing baggage gets a fresh empty filter
sized for the new draw. A root uses its first draw for its empty emitted filter
as well as its outgoing window. Early leaf checkpoints retain the intended
distance of their incoming window. Concurrent windows can have different
sizes without mutating shared geometry. SB retains window-relative ordinal
positions and resets its window to position zero at a TTL checkpoint.

## Build options and compatibility

Both one-shot scripts accept the bounds:

```bash
utils/build_deploy_dsb.sh -s docker_cgpb_es -n cgpb_ttl \
  --extra ttl --cpd-min 2 --cpd-max 6 --gc natural --collector passthrough \
  --anti-affinity --wrk2api-deploy --no-pin-requests

utils/build_deploy_hotel.sh -s docker_cgpb_es -n cgpb_ttl \
  --cpd-min 2 --cpd-max 6 --gc natural --collector passthrough
```

The scripts write the bounds into the collector image configuration. They
reject invalid bounds before generating or building the application. No service
implementation changes are needed. The `rc` random-checkpoint control is a
different algorithm and does not implement this TTL protocol.

With only `cpd`, the existing fixed-distance behavior and baggage format remain
unchanged. Explicit equal bounds enable the TTL protocol with a fixed distance.
The prefix has no in-band version marker: all SDKs on a call path and downstream
decoders must use the same format and must support the protocol before range
mode is enabled. The earlier experimental TTL format used maximum-sized Blooms
without a window descriptor; it is incompatible with the corrected PB/CGPB
range format. Reverse checkpoint segments preserve the complete descriptor
and filter within each original truss, including when different window sizes
are concatenated at fan-in. Envelope-level legacy AMQ parameters do not
describe those embedded filters.
Rebuild the collector to include its new bounds validation and rebuild the
application images to include the updated SDK. Configuration is fetched at SDK
startup; changing it requires restarting the affected processes.

The initial Kubernetes comparison in
`/users/tomislav/deployments/dsb-sn/cgpb-es-random-cpd-20260910/RESULTS.md`
used maximum-sized filters. Its byte results describe that superseded policy.

The corrected Kubernetes rerun rebuilt all fourteen Social Network SDK images
and compared 99 matching Lua requests per setting. Live traces verified Bloom
widths of 3, 5, 8, 10, and 12 bytes for selected distances 2 through 6. Random
2–6 used 243.62 exported bridge bytes per request, including window descriptors.
That run finished with 35 Ready pods and no SDK drops; rejection was disabled.
These measurements predate the reverse TTL implementation and do not measure it. See
[the corrected results](/users/tomislav/deployments/dsb-sn/cgpb-es-window-cpd-20260910/RESULTS.md)
for the measurements, build records, and limitations.

## Validation

Race-enabled tests cover PB/CGPB/SB paths, TTL 0/255 boundaries, concurrent
fanout across multiple window transitions, shrinking and growing filters,
truncated descriptors, original reverse-checkpoint locations and payloads,
mixed-size reverse fan-in, existing wrapper transport, and collector config
discovery. The deployment-option tests cover validation and switching between
fixed and ranged configuration.

The full contrib collector binary was built and exercised through its real
logs/config pipeline. Each of PB, CGPB, and SB fetched range 2–6 and processed
a 30-span path with the expected countdown and checkpoint sequence. Binary
configuration validation accepted 2–6 and rejected a maximum of 257.
