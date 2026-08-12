#!/usr/bin/env bash
# profile_edge.sh <label> — perf-profile the edge Go process under load.
#
# Two traps this avoids:
#  1. crictl's first "pid" field is the shim/sandbox (1 thread, idle) -> 0 samples.
#  2. After a rollout the OLD dying process still matches by name; profiling it
#     yields ~200 teardown samples (zap_pte_range, free_unref_page_list) instead
#     of ~170k of real work. Pick the process with the MOST THREADS.
# Requires on the node: kernel.perf_event_paranoid=-1, kptr_restrict=0.
set -uo pipefail
LBL=${1:-run}
NODE=${NODE:-node-2}
PROC=${PROC:-edge_service_nt_es_proc}
cd "$(dirname "$0")"
export REQ_DIST=fixed REQ_SIZE=${SZ:-1000} RES_DIST=fixed RES_SIZE=${SZ:-1000}
wrk -t 16 -c 1024 -d 45s -L -s workload/payload.lua http://10.10.1.3:11011 -R 90000 \
  >/tmp/prof_load_$LBL.txt 2>&1 &
sleep 8
ssh -o StrictHostKeyChecking=no "$NODE" "PROC=$PROC LBL=$LBL bash -s" <<'REMOTE' 2>/dev/null
  best=""; bestn=0
  for p in $(pgrep -f "$PROC"); do
    n=$(ls /proc/$p/task 2>/dev/null | wc -l)
    [ "$n" -gt "$bestn" ] && { bestn=$n; best=$p; }
  done
  echo "  pid=$best threads=$bestn age=$(ps -o etimes= -p $best 2>/dev/null | tr -d ' ')s"
  sudo perf record -F 997 --call-graph fp -p $best -o /tmp/perf_$LBL.data -- sleep 22 2>&1 | tail -1
  sudo perf report -i /tmp/perf_$LBL.data --no-children --stdio 2>/dev/null \
    | grep -E '^ +[0-9]+\.[0-9]+%' > /tmp/syms_$LBL
  echo "  samples/symbol-lines: $(wc -l </tmp/syms_$LBL)"
  echo "  --- kernel vs user ---"
  sudo perf report -i /tmp/perf_$LBL.data --no-children --sort dso --stdio 2>/dev/null \
    | grep -E '^ +[0-9]+\.[0-9]+%' | head -4
  echo "  --- aggregated by area (self time %) ---"
  awk '{gsub(/%/,"",$1); pct=$1; s=tolower($0);
    if (s ~ /nft_|nf_hook|conntrack|comment_mt|ip_vs|ipt_|xt_/) nft+=pct;
    else if (s ~ /futex|osyield|procyield|lock2|unlock2|semacquire|semrelease|notesleep|notewakeup|spinning|_raw_spin/) lock+=pct;
    else if (s ~ /mallocgc|gcdrain|scanobject|markroot|memclr|makeslice|growslice|gcbgmark|sweep|newobject|heapbits|spanscan|gcmark/) gcmem+=pct;
    else if (s ~ /syscall|sock_|tcp_|ip_rcv|ip_output|ip_finish|napi|softirq|skb|netif|epoll|__fget|vfs_|sendto|recvfrom|bh_enable/) net+=pct;
    else if (s ~ /findrunnable|schedule|execute|goready|newproc|morestack|park_m|stopm|startm|pick_next|put_prev/) sched+=pct;
    else if (s ~ /proto|grpc|http2|hpack/) rpc+=pct;
    else if (s ~ /memmove|copy_user/) cp+=pct;
    else other+=pct }
    END{printf "    netfilter/conntrack/kube-proxy : %6.1f%%\n    alloc + GC                    : %6.1f%%\n    syscall/net/kernel            : %6.1f%%\n    lock/spin contention          : %6.1f%%\n    proto/grpc/http2              : %6.1f%%\n    data copying (memmove)        : %6.1f%%\n    scheduler                     : %6.1f%%\n    unattributed                  : %6.1f%%\n", nft, gcmem, net, lock, rpc, cp, sched, other}' /tmp/syms_$LBL
  echo "  --- top 12 symbols ---"
  head -12 /tmp/syms_$LBL | sed 's/^/   /'
REMOTE
wait
awk '/^Requests\/sec/{print "  achieved during profile: "$2}' /tmp/prof_load_$LBL.txt
