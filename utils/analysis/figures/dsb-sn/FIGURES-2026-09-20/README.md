# FIGURES-2026-09-20 (Tomislav-RetCtx)

Same conventions as before: final on-page size, 3.33 in wide (one column of a two-column
CS conference paper), 8 pt axis labels, 7 pt ticks and legend, sans-serif.

## Optimized SDK build, n=1, ALL FOUR MEASURED IN ONE SESSION
- optsdk-latency-vs-throughput.*   mean and p99 vs ACHIEVED throughput

  build: commit f57a8699 -- packed reverse-truss encoding (_rc, no JSON envelope) and the
  reverse span state moved off the OTel span into a sharded table beside the processor.

  roots (all 2026-09-20, admission collectors 1CPU/256Mi, tuned store, CPD 2..6):
    none            retctx-nwe2e-ctrl-nt-20260920T043134Z
    vanilla         retctx-nwe2e-ctrl-v-20260920T040622Z
    PB path off     retctx-nwe2e-optsdk-off-20260920T032926Z
    PB path on      retctx-nwe2e-optsdk-on-20260920T030012Z

  capacity (req/s): None 11,974 | Vanilla 8,986 | PB off 7,793 | PB on 7,603

  decomposition:  none -> vanilla  -25.0%   (tracing at all)
                  vanilla -> off   -13.3%   (bridge forward path)
                  off -> on         -2.4%   (RESPONSE PATH; was -19.4% before the work)

  WHY ONE SESSION MATTERS. The same configurations measured 2026-09-16/18 give none 12,075,
  vanilla 9,390, PB off 8,225, PB on 6,629. Tonight none is within 0.8% but vanilla is 4.3%
  low and PB off 5.3% low -- and no-tracing runs no instrumentation at all, so that spread is
  not a regression in this build, it is the instrumented path being slower tonight. Within a
  single campaign, repetitions agree to ~1%. So cross-sitting comparisons are unsafe at the
  few-percent level and these four are not cross-sitting.

  n=1. A 40-ramp n=5 matrix on this same build is running (matrix_n5.sh), alternating
  reverse on/off single-repetition campaigns so each round yields a balanced set and the
  paired per-round gap carries its own error bar.

  command:
    utils/plot_dsb_sn_latency_throughput.py --out <nt root> --figures <this dir> \
      --extra <v root>:v \
      --extra "PB path off=<off root>:pb" \
      --extra "PB path on=<on root>:pb" \
      --name optsdk-latency-vs-throughput

  The LABEL=ROOT:KIND form was added for this figure: plain --extra keys on kind and so
  cannot hold two roots of the same kind, which is exactly what response-path on vs off is.
