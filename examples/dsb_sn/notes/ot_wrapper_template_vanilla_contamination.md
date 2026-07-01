# OT wrapper codegen hardcoded the path-bridge template for ALL variants → "vanilla" was not a clean baseline

**Date:** 2026-06-30
**Status:** FIXED (per-variant template selection via `OT_BRIDGE`), rebuild + revalidation in progress.

## Observation
Across every fourway sweep (masked n=10 AND anti-affinity n=5/10), vanilla never came
out cleanly cheapest — it sat on top of or among the bridges, sometimes "worst,"
which is anti-physical (vanilla does strictly less work than any bridge). Flat-region
deltas were within run-to-run noise, but vanilla's *position* flipped between regimes
(cheapest masked, dearest anti-affinity) — a sign the comparison wasn't clean.

## Characterization
The OpenTelemetry plugin codegen (`plugins/opentelemetry/ir_ot_client.go`,
`ir_ot_server.go`) has FOUR per-variant wrapper templates —
`clientSideTemplate{Vanilla,Path,CGPB,SBridge}` and the server equivalents — but the
template selection was **hardcoded**: line 166 always emitted `clientSideTemplatePath`
and line 216 always `serverTemplatePath`, with the other three commented out. Codegen
runs once per build (`go run wiring/main.go`) and never saw the bridge kind (that's the
runtime `BRIDGE_KIND` env). So **every build — v_es, pb_es, cgpb_es, sb_es, rc_es —
emitted the identical PATH-BRIDGE client/server wrappers.** The variant differed only
by the runtime SpanProcessor.

Consequence: "vanilla" ran the path-bridge instrumentation wrapper on every client/
server call. Live (uncommented) diff of `clientSideTemplatePath` vs
`clientSideTemplateVanilla` = exactly two extra executable lines per client call:

```go
childCountPtr := ctx.Value("childCount").(*atomic.Uint64)
ctx = context.WithValue(ctx, "seqNum", int(childCountPtr.Add(1)))
```

i.e. a `ctx.Value` lookup + type assertion, an **atomic increment**, and a
**`context.WithValue` allocation** (new heap context → GC pressure) on every traced
client hop — many per request in DSB-SN. Everything else (baggage map, `__bag.` scan,
span Start) is identical. So vanilla carried real, if modest, per-call overhead it
should not have, and was therefore not a valid no-bridge baseline. pb was correct
(Path == path bridge); cgpb (CGPB) and sb (SBridge) were ALSO mis-emitted as Path —
sb notably lost its `endEvents`/`ccMutex` wrapper setup, though its processor's
delayed-end path is self-contained (`delayedEndEventsChan`) so it wasn't functionally
broken, just carrying an unneeded childCount atomic.

## Hypothesis
If the codegen selects the correct per-variant template, vanilla loses the
childCount-atomic + context-allocation per call and should drop below the bridges in
CPU (the metric that resolves the overhead; latency can't at this scale).

## Fix
`ir_ot_client.go:166` / `ir_ot_server.go:216`: replace the hardcoded template with a
switch on `os.Getenv("OT_BRIDGE")` (v→Vanilla, pb/path→Path, cgpb→CGPB, sb→SBridge,
default Path). `build_deploy_dsb.sh` stage [1] derives `OT_BRIDGE` from the spec
(`docker_<kind>_es` → `<kind>`) and exports it before `go run wiring/main.go`, so the
codegen template and the runtime `BRIDGE_KIND` always agree.

## Validation (falsifying experiment)
- Canary: full rebuild of v_esrev2 with `OT_BRIDGE=v` → compiled + built + pushed clean
  (the long-dormant Vanilla template is NOT bit-rotted). Generated wrapper
  `docker/.../ot/*_OTClientWrapperInterface.go` confirmed **childCount=0, atomic=0**,
  baggage + span Start retained.
- pb/cgpb/sb rebuilds with their correct templates: in progress.
- Next: re-run the CPU-sampled anti-affinity fourway. Prediction: true-vanilla CPU
  per service drops below the bridges, monotonic v < sb < pb < cgpb at the processor
  layer (overhead finally clean in the CPU metric).

## Impact on prior results
Every fourway sweep before this fix had vanilla (and sb/cgpb) on the wrong wrapper, so
they are invalid as the overhead baseline and are discarded. pb runs were wrapper-
correct. Re-running the full fourway from the corrected codegen.
