#!/usr/bin/env bash
# Tomislav-RetCtx: resume the SN memory-limit bridges root after the user stopped vanilla at its plateau: pb cgpb sb.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
echo $$ > $S/snnw_mem_chain.pid
B=$(cat $S/snnw_mem_opt_root.txt)
echo "$(date -u +%H:%M:%S) resume pb cgpb sb $B"
bash $S/run_only.sh $B
echo "$(date -u +%H:%M:%S) SNNW MEM CHAIN COMPLETE"
