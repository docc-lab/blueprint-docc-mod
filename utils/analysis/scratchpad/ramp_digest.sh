#!/usr/bin/env bash
# Tomislav-RetCtx: low-noise event stream for a long ramp campaign. Emits: stage changes;
# per case the FIRST point with refusals, the knee (first mean > 200 ms) and any point with
# failed requests; a digest line (latest point) every 5 minutes; terminal state.
# Usage: ramp_digest.sh <root> <mode> [namespace]
R=$1; MODE=${2:-run}; NS=${3:-dsb-hotel}
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
seen=$(mktemp); flags=${FLAGS:-$(mktemp)}; touch $flags; prev=""; last=0; latest=""
[ -n "${SKIP_EXISTING:-}" ] && find $R/$MODE/ -mindepth 3 -maxdepth 3 -path "*/rate-*" -name result.json 2>/dev/null > $seen
while true; do
  for f in $(find $R/$MODE/ -mindepth 3 -maxdepth 3 -path "*/rate-*" -name result.json 2>/dev/null | grep -v -- "-interrupted-" | sort); do
    grep -qx "$f" $seen && continue
    # wait until the runner has added the collector deltas (second write of result.json)
    python3 -c "import json,sys; sys.exit(0 if 'collector_deltas' in json.load(open('$f')) else 1)" 2>/dev/null || continue
    echo "$f" >> $seen
    out=$(python3 - "$f" "$flags" <<'EOF'
import json, sys
f, flags = sys.argv[1:]
d = json.load(open(f)); c = d.get('collector_deltas', {})
acc = c.get('otelcol_receiver_accepted_spans_total', 0); ref = c.get('otelcol_receiver_refused_spans_total', 0)
err = d.get('non_2xx_3xx', 0) + sum((d.get('socket_errors') or {}).values())
case = f.split('/')[-3]; rate = d['offered_rps']
line = (f"{case} @{rate}: got {d.get('completed_rps', 0):,.0f} rps  mean {d.get('mean_ms', 0):,.1f} ms  "
        f"p99 {d.get('p99_ms', 0):,.1f} ms  err {err}  refused {100 * ref / max(1, acc + ref):.1f}%")
seen = set(open(flags).read().split())
events = []
if err: events.append('ERRORS')
if ref > 0 and f'{case}:ref' not in seen: events.append('FIRST REFUSALS'); seen.add(f'{case}:ref')
if d.get('mean_ms', 0) > 200 and f'{case}:knee' not in seen: events.append('KNEE (mean > 200 ms)'); seen.add(f'{case}:knee')
open(flags, 'w').write(' '.join(seen))
print(('EVENT ' + ', '.join(events) + ' | ' if events else 'LATEST ') + line)
EOF
)
    case "$out" in EVENT*) echo "$(date -u +%H:%M:%S) $out";; esac
    latest="$out"
  done
  s=$(python3 -c "import json;d=json.load(open('$R/$MODE-status.json'));print(d.get('state'),d.get('repetition',''),d.get('case',''),d.get('stage',''),str(d.get('error',''))[:400])" 2>/dev/null)
  st=$(echo "$s" | awk '{print $1,$2,$3,$4}')
  if [ -n "$s" ] && [ "$st" != "$prev" ]; then echo "$(date -u +%H:%M:%S) STAGE $s"; prev="$st"; fi
  case "$s" in complete*|failed*) exit 0;; esac
  if ! pgrep -f "run_dsb_sn_nw.py $MODE --out $R" >/dev/null; then sleep 5; echo "RUNNER GONE: $(cat $R/$MODE-status.json 2>/dev/null | tr -d '\n' | head -c 500)"; exit 1; fi
  if [ $(( $(date +%s) - last )) -ge 300 ]; then
    live=$(NS=$NS MODE=$MODE python3 $S/live_line.py $R 2>/dev/null)
    echo "$(date -u +%H:%M:%S) DIGEST ${latest#LATEST } || ${live#* }"; last=$(date +%s)
  fi
  [ -n "${CHAINLOG:-}" ] && grep -q -E "Traceback|Error" "$CHAINLOG" 2>/dev/null && { grep -E "Error" "$CHAINLOG" | tail -1 | cut -c1-600; exit 1; }
  sleep 15
done
