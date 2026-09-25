#!/usr/bin/env bash
# Tomislav-RetCtx: event stream for a ramp campaign root (any app): one POINT line per finished
# rate (achieved rps, mean/p99 latency, errors, agent refusal share), stage changes of the runner,
# and the terminal state (complete / failed / runner gone). Usage: ramp_monitor.sh <root> <mode>
R=$1; MODE=${2:-run}; NS=${3:-dsb-hotel}
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
seen=$(mktemp); prev=""; last=$(date +%s)
# SKIP_EXISTING=1: do not re-report points that already exist (re-armed monitor)
[ -n "${SKIP_EXISTING:-}" ] && find $R/$MODE/ -mindepth 3 -maxdepth 3 -path "*/rate-*" -name result.json 2>/dev/null > $seen
while true; do
  for f in $(find $R/$MODE/ -mindepth 3 -maxdepth 3 -path "*/rate-*" -name result.json 2>/dev/null | grep -v -- "-interrupted-" | sort); do
    grep -qx "$f" $seen && continue
    echo "$f" >> $seen; last=$(date +%s)
    python3 - "$f" <<'EOF'
import json, sys
f = sys.argv[1]; d = json.load(open(f))
c = d.get('collector_deltas', {})
acc = c.get('otelcol_receiver_accepted_spans_total', 0); ref = c.get('otelcol_receiver_refused_spans_total', 0)
err = d.get('non_2xx_3xx', 0) + sum((d.get('socket_errors') or {}).values())
rep_case = f.split('/')[-3]
print(f"POINT {rep_case} @{d['offered_rps']}: got {d.get('completed_rps', 0):,.0f} rps  mean {d.get('mean_ms', 0):,.1f} ms  "
      f"p99 {d.get('p99_ms', 0):,.1f} ms  err {err}  refused {100 * ref / max(1, acc + ref):.1f}%  "
      f"restarts_changed {d.get('restarts_changed')}", flush=True)
EOF
  done
  s=$(python3 -c "import json;d=json.load(open('$R/$MODE-status.json'));print(d.get('state'),d.get('repetition',''),d.get('case',''),d.get('stage',''),str(d.get('error',''))[:400])" 2>/dev/null)
  st=$(echo "$s" | awk '{print $1,$2,$3,$4}')
  if [ -n "$s" ] && [ "$st" != "$prev" ]; then echo "$(date -u +%H:%M:%S) STAGE $s"; prev="$st"; fi
  case "$s" in complete*|failed*) exit 0;; esac
  if ! pgrep -f "run_dsb_sn_nw.py $MODE --out $R" >/dev/null; then sleep 5; s=$(python3 -c "import json;print(json.load(open('$R/$MODE-status.json')))" 2>/dev/null); echo "RUNNER GONE: $s"; exit 1; fi
  # never silent: a live fleet line when nothing else was reported for 2 minutes
  if [ $(( $(date +%s) - last )) -ge 120 ]; then NS=$NS MODE=$MODE python3 $S/live_line.py $R 2>/dev/null || echo "$(date -u +%H:%M:%S) live sample failed"; last=$(date +%s); fi
  [ -n "${CHAINLOG:-}" ] && grep -q -E "Traceback|Error" "$CHAINLOG" 2>/dev/null && { grep -E "Error" "$CHAINLOG" | tail -1 | cut -c1-600; exit 1; }
  sleep 15
done
