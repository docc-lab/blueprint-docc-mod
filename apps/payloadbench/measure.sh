#!/usr/bin/env bash
# measure.sh <label> <url> [size] — throughput + TRUE cpu (cgroup cpu.stat).
# Values are passed to python as argv, never interpolated into an f-string
# (shell-substituting a label inside f'{...}' is a SyntaxError).
set -uo pipefail
cd "$(dirname "$0")"
LBL=$1; URL=$2; SZ=${3:-1000}
export REQ_DIST=fixed REQ_SIZE=$SZ RES_DIST=fixed RES_SIZE=$SZ
UID_=$(kubectl get pods --no-headers -o custom-columns=:metadata.name,:metadata.uid,:metadata.deletionTimestamp \
        | awk '/^edge-service/ && $3=="<none>"{print $2}' | head -1)
wrk -t 8 -c 128 -d 12s -L -s workload/payload.lua "$URL" -R 5000 >/dev/null 2>&1
wrk -t 16 -c 128 -d 26s -L -s workload/payload.lua "$URL" -R 250000 >/tmp/m_$LBL.txt 2>&1 &
sleep 6
CPU=$(ssh -o StrictHostKeyChecking=no node-2 "
  cg=\$(find /sys/fs/cgroup -maxdepth 4 -type d -name '*${UID_//-/_}*' 2>/dev/null | head -1)
  a=\$(awk '/usage_usec/{print \$2}' \$cg/cpu.stat); sleep 12
  b=\$(awk '/usage_usec/{print \$2}' \$cg/cpu.stat)
  echo \"scale=3; (\$b-\$a)/12000000\" | bc" 2>/dev/null)
wait
A=$(awk '/^Requests\/sec/{print $2}' /tmp/m_$LBL.txt)
P50=$(awk '/^ 50.000%/{print $2}' /tmp/m_$LBL.txt)
BAD=$(awk '/Non-2xx/{print $NF}' /tmp/m_$LBL.txt); BAD=${BAD:-0}
python3 - "$LBL" "${A:-0}" "${CPU:-0}" "$P50" "$BAD" <<'PY'
import sys
lbl, a, c, p50, bad = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), sys.argv[4], sys.argv[5]
per = 1e6 * c / a if a else 0
print(f"MEASURE {lbl:<26} achieved={a:<10.0f} edge_cpu={c:<6.2f} cores  cpu_per_req={per:<6.1f} us  p50={p50:<9} non2xx={bad}")
PY
