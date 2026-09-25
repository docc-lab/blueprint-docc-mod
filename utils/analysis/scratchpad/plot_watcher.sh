#!/usr/bin/env bash
# Tomislav-RetCtx: regenerate the matrix figures each time a ROUND completes.
# A round counts only when both arms are done, so every figure is a balanced n.
# Detached-safe: launch under setsid.
set -eu
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
PY=/users/tomislav/blueprint-docc-mod/.venv/bin/python
LOG=$S/plot-watcher.log
last=0
echo "$(date -u +%H:%M:%S) watcher started" >> "$LOG"
while true; do
  n=$($PY -B - <<'PY' 2>/dev/null || echo 0
import json, re
from pathlib import Path
S = Path('/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad')
f = S / 'matrix_roots.txt'
found = {}
if f.exists():
    for line in f.read_text().split():
        root = Path(line)
        m = re.search(r'-m5-(on|off)-r(\d+)-', root.name)
        if not m or not (root / 'run-complete.json').exists():
            continue
        try:
            if json.loads((root / 'run-complete.json').read_text())['passed']:
                found.setdefault(int(m.group(2)), set()).add(m.group(1))
        except Exception:
            pass
print(sum(1 for a in found.values() if a == {'on', 'off'}))
PY
)
  if [ "${n:-0}" -gt "$last" ]; then
    echo "$(date -u +%H:%M:%S) round $n complete -> regenerating figures" >> "$LOG"
    ok=1
    $PY -B "$S/regen_matrix_plots.py" >> "$LOG" 2>&1 || ok=0
    # Also the eight-series views (all kinds, both x axes, on/off by line style).
    $PY -B "$S/plot_all_series.py" >> "$LOG" 2>&1 || ok=0
    if [ "$ok" = 1 ]; then echo "$(date -u +%H:%M:%S) figures for n=$n written" >> "$LOG"
    else echo "$(date -u +%H:%M:%S) REGEN FAILED for n=$n" >> "$LOG"; fi
    last=$n
  fi
  [ "${n:-0}" -ge 5 ] && { echo "$(date -u +%H:%M:%S) all five rounds plotted; watcher exiting" >> "$LOG"; break; }
  pgrep -f "matrix_n5.sh" >/dev/null 2>&1 || { echo "$(date -u +%H:%M:%S) matrix driver gone; watcher exiting at n=$n" >> "$LOG"; break; }
  sleep 120
done
