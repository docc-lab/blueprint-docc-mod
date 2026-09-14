# Probabilistic reverse checkpointing

Tomislav-RetCtx: PB, CGPB, and SB support probability-based reverse emission as
an alternative to reverse TTLs. Each ordinary receiving span makes an independent
decision for **each truss**. Accepted trusses share that span's checkpoint export;
unaccepted trusses continue upstream. The existing TTL policy remains the default.

## Policies

Let `n` be the rejected leaf's absolute span depth and `d` the receiving span's
absolute depth, with the root at zero. Depth counts both client and server spans;
the potential receivers have depths `n-1` down to `0`. Each truss already carries
its own `n`, so fan-in never substitutes the deepest sibling's depth.

| `reverse_policy` | Acceptance probability at an ordinary receiver |
| --- | --- |
| `ttl` | Existing per-truss countdown; no probability trial |
| `probability` | Configured `reverse_probability`, in `[0,1]` |
| `inverse_depth` | `1/n`, constant along this truss's return path |
| `depth_linear` | `2(d+1) / (n(n+1))`, increasing with receiver depth |

For `n=6`, the conditional probabilities are:

| Receiver depth | `inverse_depth` | `depth_linear` |
| --- | --- | --- |
| 5: immediate parent | 16.67% | 28.57% |
| 3 | 16.67% | 19.05% |
| 1 | 16.67% | 9.52% |

Tomislav-RetCtx: the linear formula normalizes weights `d+1` by their sum
`n(n+1)/2`. Before mandatory checkpoint absorption, both its per-node
probabilities and the `1/n` probabilities sum to one over the potential receivers.
This makes their overall scale comparable while favoring deeper receivers.
It does **not** make them a categorical distribution of final emission locations.

These are conditional trials: a node is tried only if the truss reaches it.
Among ordinary receivers, even `1/n` gives deeper nodes more first-success mass.
Mandatory absorption can still make the original checkpoint the most common emitter.
Let `c` be the first original checkpoint upstream of a leaf at depth `n`:

```text
P(emit at d) = p(d,n) * product[k=d+1..n-1](1 - p(k,n)),  c < d < n
P(emit at c) = product[k=c+1..n-1](1 - p(k,n))
```

An empty product equals one. Original checkpoints and the root absorb all
remaining returns even for configured probability zero. The linear formula
increases preference for deeper ordinary receivers without changing those
boundaries. It does not promise lower tracing bytes for every topology.

## Configuration and lifecycle

Select the policy in the collector's existing config-discovery receiver:

```yaml
receivers:
  configdiscovery:
    endpoint: ":8080"
    config_map:
      cpd_min: 2
      cpd_max: 6
      reverse_policy: depth_linear
```

For a fixed probability, use `reverse_policy: probability` and add
`reverse_probability: 0.25`. Other policies reject a supplied
`reverse_probability`; missing/nonfinite/out-of-range fixed probabilities and
unknown policy names fail validation in both collector and SDK. SDKs fetch
configuration at startup. Forward checkpoint distance and Bloom sizing continue
to use `cpd` or `cpd_min`/`cpd_max`, independently of this reverse policy.

Both one-shot scripts accept `--reverse-policy` and `--reverse-probability`.
Switching away from fixed probability removes its stale configuration value;
omitting both options preserves a custom collector configuration. For example:

```bash
utils/build_deploy_hotel.sh -s docker_cgpb_es -n cgpb_reverse_probability \
  --cpd-min 2 --cpd-max 6 --reverse-truss --rt-leaf-reject 1 \
  --reverse-policy depth_linear --collector passthrough --gc natural
```

`REVERSE_TRUSS=on` and a positive `RT_LEAF_REJECT` still enable leaf rejection.
`RT_LEAF_REJECT` controls **whether an unscheduled leaf returns a truss**;
`reverse_probability` controls **whether an upstream receiver accepts one**.
Social Network's script configures the collector policy; set its application
rejection environment separately. The old `RT_POLICY`/`RT_DEPTH` variables remain
deprecated and do not select these policies.

## Carrier and compatibility

Tomislav-RetCtx: probability-mode leaves return the existing origin ID, varint
depth, and exact truss bytes **without a reverse TTL**. No probability/depth field
is added to the carrier. TTL-free checkpoint segments use the receiving SDK's
configured probability policy; under the default TTL policy they wait for an
original checkpoint. Explicit TTL segments retain their countdown even when
mixed with probability-mode returns. TTL-free ancestry-only or malformed segments
receive no probability trial and pass through until an original checkpoint.

Use consistent SDK policies along a call path when comparing experiments: the
carrier does not pin a probability policy at its origin. Fan-in performs no
trial. Repeated SDK preparation cannot sample again, and an emitting span
forwards its unaccepted trusses without resetting the forward window. Failed
trials that leave the whole bundle pending avoid reserializing it.

Tests cover formulas and bounds, independent trials, distinct sibling depths,
mixed TTLs, malformed input, real SDK export classification, collector discovery,
deployment configuration, and 79-span concurrent trees across all policies.
On September 13, 2026, SDK/backend/wrapper and collector-receiver race tests
passed, along with all 13 deployment-option tests and shell syntax checks.
This revision is tested locally; it has not been deployed or measured under load.
See [SDK handoff and encoding](reverse_truss.md) and the
[session change map](tomislav_retctx_changes.md).
