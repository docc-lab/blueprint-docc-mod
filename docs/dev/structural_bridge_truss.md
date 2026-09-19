# Tomislav-RetCtx: S-Bridge on the CG-Bridge core

Written 2026-09-15. Replaces the S-Bridge's former ordinal-plus-parent-fingerprint
chain with the paper's design (bridges.pdf §3.4, Figure 4): the CG-Bridge
topological core, plus the S-Bridge's vertical start ordinals, plus orthogonal end
events and delayed end events, with Lehmer-coded end-event permutations.

## Wire format

One packed S-Bridge payload (`runtime/plugins/otelcol/structural_truss.go`):

```text
varint(absolute depth) || checkpoint root (8) || byte(window distance - 1)
|| bloom[geometry(distance)]                          CG core: ancestor AMQ
|| varint(len HA) || HA                               CG core: fan-out witnesses
|| varint(n) || n x ( varint(ordinal) || end_group(bound = ordinal - 1) )
|| varint(m) || m x ( trace_id (16) || parent span (8) || varint(children) || end_group(bound = children) )
```

Forward baggage `_br` prefixes the mutable TTL byte exactly as ranged PB/CGPB do.
The first four fields are byte-identical to the ranged PB/CGPB core, so the shared
`unpackCheckpointWindowBR` reads an S-Bridge payload's depth, anchor, distance and
Bloom unchanged. The HA is length-prefixed here (CGPB leaves it as the trailing
remainder) so that the orthogonal tail can follow it. HA entries and the
first/second-child witness rule are the CGPB ones, unchanged.

- **Ordinals (vertical).** One start ordinal per span in the window since the last
  checkpoint, in path order, ending with the span carrying the payload. Bounded by
  the window distance, as Figure 4 says. A checkpoint persists the list and
  propagates an empty one.
- **End events (orthogonal, recorded vertically).** Position `i`'s group is the
  list of sibling start ordinals that had ended before span `i` started, as seen by
  its parent. The parent is implied by the position (the previous window span or
  the anchor), which is what the paper's "records ... its parent's fingerprint"
  provides. Bound: the span's own ordinal minus one.
- **Delayed end events.** Ends a parent observed after its last child started,
  minus the implied last one, stamped with the parent's trace and span ID (the
  paper's `T:S:...`), queued in the service instance and attached to the next
  outgoing span there. They ride vertically until a checkpoint captures them, then
  are not propagated further.
- **Non-checkpoint spans** emit `_o` (own ordinal) and `_d` (absolute depth) as
  proto bytes, as PB/CGPB emit `_d`.

### end_group and Lehmer coding

`varint(count << 1 | lehmer)`. An empty group is the single byte `0`. With
`lehmer = 1`: a bitmap of `bound` bits selects which ordinals ended, then the
permutation of that set in end order as Lehmer digits packed into mixed-radix
uvarint words (chunked so every word stays below 2^62; encoder and decoder derive
the chunk boundaries from the count alone). Six ended siblings cost 4 bytes.
With `lehmer = 0`: `count` plain varints. Lehmer coding is a compile-time
choice, on by default; build every service with `-tags sb_nolehmer` (for the
generated Dockerfiles: `GOFLAGS=-tags=sb_nolehmer` at image build) for the
plain-list ablation. Plain is also used when the ends are not a distinct subset
of `1..bound`.

## Start ordinals

The SB wrapper templates (`plugins/opentelemetry/ir_ot_client.go`,
`ir_ot_server.go`) now number children by a per-request `childCount` counter, so
ordinals are the paper's start order `1..k`. The previous SB templates incremented
an `eventCount` on both start and end, which produced gapped ordinals. The server
wrapper's end-of-request summary is now `varint(children) || varint(count) ||
ordinals` (last end dropped). The `childCount` attribute doubles as the leaf
signal, as in CGPB.

## Checkpoint classification and reverse trusses

SB spans carry `__bag.prio` from `OnStart` like PB/CGPB and use the shared
`isPathCheckpoint` rule (scheduled window checkpoint, or childless server span,
unless the reverse SDK rejected it; `__bag.force_lp` still forces ordinary).
Because the payload now carries absolute depth, the reverse SDK reads SB depth from
`_br` like PB/CGPB; the auxiliary `__rt_depth` baggage is no longer needed for SB.
Returned SB trusses (`checkpoint.sb`) contain the exact emitted payload.

Fixed-distance (`cpd`) deployments use the same layout with `distance = cpd` and
no TTL byte.

## Compatibility

This is a breaking format change for any external decoder of the former SB
`_br` (ordinal groups keyed by window depth with full parent fingerprints, flat
end-event list, `trace_id || depth` delayed triples). The reconstruction decoder in
the bridges repository must follow. The analyzer (`utils/analyze_dsb_sn_e2e.py`,
`inspect_traces`) validates the new layout, including Lehmer groups, and counts end
events, delayed trusses and witnesses per sample.

## Open question inherited from CGPB

The `__seq` baggage gives a server span the same ordinal as its client parent, so
a second child's server span records a fan-out witness naming the client span,
which has exactly one child. CGPB has always done this; SB now inherits it. Whether
reconstruction treats client/server pairs as one hop, or the witness rule should
skip server spans, is a design decision to settle with the reconstruction side.
