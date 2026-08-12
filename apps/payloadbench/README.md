# PayloadBench

A deliberately minimal two-service Blueprint application for measuring the
effect of inter-service payload size on throughput and response time, with the
forward path (edge→internal) and return path (internal→edge) controlled
independently per request. Companion microbenchmark for the return-path
context-propagation work (see `examples/dsb_sn/notes/return_path_propagation_design.md`).

* [workflow](workflow) — the two services:
  * `EdgeService.Call(ctx, reqSize, resSize) error` — HTTP entry point. Generates
    `reqSize` bytes, sends them to the internal service, asks for `resSize` bytes
    back, and returns only OK / not-OK (HTTP 200 / 500). The client request stays
    tiny, so the measured cost is purely the edge↔internal payload in each direction.
  * `InternalService.Echo(ctx, payload, resSize) ([]byte, error)` — gRPC backend;
    receives the forward payload, returns `resSize` bytes.
  * Payloads are zero-filled sub-slices of a shared 16 MiB buffer (no per-request
    allocation ≤ 16 MiB; byte values don't matter without compression, only length).
* [wiring](wiring) — variants mirroring `examples/dsb_sn` (same deployment stack:
  retries=3, clientpool=100 on the HTTP frontend, OTel instrumentation,
  grpc/http + goproc + linuxcontainer, otelcol → jaeger [→ Elasticsearch]):
  `docker_{v,pb,cgpb,sb}[_es]` plus `docker_nt_es` (no app-side tracing).
* [workload](workload) — `payload.lua` for wrk/wrk2: per-request `reqSize`/`resSize`
  drawn from env-configured distributions (fixed/uniform/normal/exp/zipf), optional
  stochastic arrival processes. See the header comment for all knobs.

## Compiling

```bash
cd apps/payloadbench/wiring
OT_BRIDGE=v go run . -w docker_v_es -o ../build_v_es
```

**The same two build-time rules as dsb_sn apply:**

1. `OT_BRIDGE=<kind>` must be exported at codegen time so the generated OT wrapper
   template matches the variant (v / pb / cgpb / sb) — otherwise the default (path)
   template contaminates the baseline (see
   `examples/dsb_sn/notes/ot_wrapper_template_vanilla_contamination.md`).
   `BLUEPRINT_BRIDGE_KIND` (runtime processor selection) is defaulted by the spec.
2. Run `goimports -w <build>/docker` after codegen — the OT wrapper generator emits
   unused imports and the docker builds fail without it. (`build_deploy_dsb.sh`
   stage [1c] does this for dsb_sn.)

## Driving load

```bash
# fixed 4 KiB forward, 64 KiB return, constant rate (wrk2 CO-corrected):
REQ_DIST=fixed REQ_SIZE=4096 RES_DIST=fixed RES_SIZE=65536 \
  wrk -t8 -c128 -d30s -R5000 -s workload/payload.lua http://EDGE:PORT
```

## Caveats

* **gRPC message cap:** the generated gRPC server/client use default options, so
  payloads over ~4 MB fail on the edge↔internal hop (forward at the server's
  receive, return at the client's receive) despite the 16 MiB generator headroom.
  Keep `reqSize`/`resSize` ≤ 4 MiB, or add `MaxRecvMsgSize` options to the grpc
  plugin templates if larger sizes are needed.
* **Retries:** `retries.AddRetries(…, 3)` is applied to match the dsb_sn stack.
  For pure payload-cost measurement note that a transient failure retries the
  full payload transfer.
