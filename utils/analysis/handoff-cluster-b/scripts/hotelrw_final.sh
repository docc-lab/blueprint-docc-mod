#!/usr/bin/env bash
# Tomislav-RetCtx (user 2026-09-24 ~21:05Z): HotelReservation REAL-WORK on cluster B after the SN real-work chain, bridges
# WITHOUT reverse_passthrough. 1) waits for the SN real-work chain (bridges + nt repetition 6) to finish -- no compiling
# during measurement; 2) builds nt v pb cgpb sb from this tree = FINAL SDK (passthrough-capable, bounded refused-trace census,
# census OFF by default) with the recipe of retctx-hotel-cubic-opt2src (prepare_dsb_hotel.py real-work, build_dsb_sn_nw.py);
# 3) derives + verifies each group against its n=1 memlimit-round root (retctx-hotel-mem-{nt,v,br}: stack / plan / manifest
# identical except rates, repetitions and app image digests; no reverse_passthrough anywhere); 4) runs n=5 with the protocol
# of every final round: a FRESH deployment per repetition (fresh databases / caches), 30 s points, repetition 1 climbs
# START.. in 1k steps to the kind's own plateau (3 points without >= 1 % gain), repetitions 2..5 re-run its grid; no census,
# no trace capture. cpd 2..4 depth_cubic (hotel default). usage: START=10000 setsid nohup hotelrw_final.sh > ... &  (DRYRUN=1)
set -euo pipefail
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin  # detached (non-login) launches do not read ~/.profile; the builds need go
H=$(cd "$(dirname "$0")/.." && pwd); ST=$H/state
REPO=/users/tomislav/blueprint-docc-mod; DH=/users/tomislav/deployments/dsb-hotel
PY="$REPO/.venv/bin/python -B -u"; DRYRUN=${DRYRUN:-}; START=${START:-10000}
COL_SRC=10.10.1.1:30000/otelcontribcol@sha256:4868878a7d79592b77db54bc1c170096b1d589764d9c05daa36631baea273e1b
D=10.10.1.1:30000/otelcontribcol@sha256:1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94  # queue3
BACKEND_MANIFEST=/users/tomislav/deployments/dsb-sn/retctx-nw-census-20260923T040259Z/builds/v/manifest.yaml
N1_NT=$DH/retctx-hotel-mem-nt-20260924T045738Z; N1_V=$DH/retctx-hotel-mem-v-20260924T051302Z; N1_BR=$DH/retctx-hotel-mem-br-20260924T051305Z
fail() { echo "$(date -u +%H:%M:%S) PASSES CHAIN FAILED: $*"; exit 1; }
if [ -z "$DRYRUN" ]; then
  echo $$ > $ST/hotelrw_final.pid
  until grep -q "PASSES CHAIN COMPLETE snrw nopt" $ST/snrw_nopt_chain.log; do
    grep -q "PASSES CHAIN FAILED" $ST/snrw_nopt_chain.log && fail "SN real-work chain failed; not starting"; sleep 20; done
  echo "$(date -u +%H:%M:%S) SN real-work chain complete; building hotel real-work (final SDK)"
  SRC=$DH/retctx-hotelrw-final-src-$(date -u +%Y%m%dT%H%MZ); echo $SRC > $ST/hotelrw_final_src_root.txt
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/prepare_dsb_hotel.py --out $SRC --collector-image $COL_SRC --kinds nt,v,pb,cgpb,sb" > $ST/hotelrw_final_prepare.log 2>&1 || { tail -5 $ST/hotelrw_final_prepare.log; fail "prepare"; }
  STAMP=$(basename $SRC | rev | cut -d- -f1 | rev | tr 'A-Z' 'a-z')
  for k in v pb cgpb sb; do
    d=$REPO/examples/dsb_hotel/build_${k}_hotel_${STAMP}
    a=$(grep -rl AppendSpanBaggage $d 2>/dev/null | wc -l); p=$(grep -rl reverse_passthrough $d 2>/dev/null | wc -l); c=$(grep -rl RETCTX_REFUSED_CENSUS $d 2>/dev/null | wc -l)
    echo "$(date -u +%H:%M:%S) $k: AppendSpanBaggage $a, reverse_passthrough $p, RETCTX_REFUSED_CENSUS $c files"
    [ "$a" -gt 0 ] && [ "$p" -gt 0 ] && [ "$c" -gt 0 ] || fail "$k build lacks hook / passthrough switch / census switch"
  done
  sg docker -c "cd $REPO && .venv/bin/python -B -u utils/build_dsb_sn_nw.py --out $SRC --backend-manifest $BACKEND_MANIFEST" > $ST/hotelrw_final_build.log 2>&1 || { tail -5 $ST/hotelrw_final_build.log; fail "image build"; }
  echo "$(date -u +%H:%M:%S) HOTEL RW FINAL BUILD COMPLETE $SRC"
else
  SRC=${SRC:?DRYRUN needs SRC=<an existing hotel real-work source root>}
fi
COMMON="--source $SRC --provenance-from $SRC --collector admissionotel500m --backend clickhouse --gateway-cpu 2 --reverse on --seconds-per-rate 30 --collector-image $D --gomaxprocs auto --us-margin-mode backlog --gateway-lp-refusal resource_exhausted --agent-compression none --app-gc-memlimit 1GiB --app-gctrace --no-trace-capture --plateau-stop --repeat-grid --repetitions 5 --rates $(seq -s ' ' $START 1000 60000)"
BR="--priority-receiver --sdk-retry priority --priority-queue --gateway-priority-queue"
NOTE="HotelReservation REAL-WORK, n=5, FRESH deployment per repetition, repetition 1 climbs ${START}.. (1k steps) to the kind's plateau, 2..5 re-run its grid; FINAL SDK images (census off), bridges WITHOUT reverse_passthrough (cpd 2..4 depth_cubic); hotel opt2 best config on ClickHouse, GOGC=off GOMEMLIMIT=1GiB gctrace; no census, no trace capture. No smoke."
ROOTS=$ST/hotelrw_roots.txt; [ -n "$DRYRUN" ] && ROOTS=$ST/hotelrw_dryrun_roots.txt; : > $ROOTS
cd $REPO/utils
TS=$(date -u +%Y%m%dT%H%M%SZ)
for group in nt v br; do
  case $group in nt) KINDS=nt; EXTRA=""; N1=$N1_NT;; v) KINDS=v; EXTRA=""; N1=$N1_V;; br) KINDS="pb cgpb sb"; EXTRA=$BR; N1=$N1_BR;; esac
  R=$DH/retctx-hotelrw-final-$group-$TS; [ -n "$DRYRUN" ] && R=$ST/dryrun/retctx-hotelrw-final-$group-$TS
  $PY derive_dsb_sn_nw.py $COMMON $EXTRA --out $R --kinds $KINDS --note "$NOTE" > $ST/hotelrw_${group}_derive.log 2>&1 || fail "derive $group (see $ST/hotelrw_${group}_derive.log)"
  for m in $R/builds/*/manifest.yaml; do grep -q GOMEMLIMIT $m || fail "$m lacks GOMEMLIMIT"; ! grep -q reverse_passthrough $m || fail "$m sets reverse_passthrough"; done
  python3 - $R $START <<'PYV' || fail "$R plan check"
import sys, json
R, start = sys.argv[1], int(sys.argv[2]); p = json.load(open(R + '/plan.json'))
assert p['backend'] == 'clickhouse' and p['app_gc_memlimit'] == '1GiB' and p['app_gctrace'], 'stack'
assert p['collector_image_override'].endswith('1358b235383ab225c4adda17fe924be08e99566ac578bdbcde5f8081fa61be94'), 'queue3'
assert p['repetitions'] == 5 and len(p['seeds']) == 5 and p.get('repeat_grid') and p['plateau_stop'] == {'flat_points': 3, 'min_gain': 0.01}, 'protocol'
assert p['ramp_rates'][0] == start and p['ramp_rates'][1] - p['ramp_rates'][0] == 1000 and not p.get('ramp_passes'), 'grid'
assert p.get('trace_capture') is False and not p.get('discovery_override'), (p.get('trace_capture'), p.get('discovery_override'))
cases = json.load(open(R + '/cases.json')); assert all(c.get('app') == 'hotel' for c in cases), [c.get('app') for c in cases]
print(f"{[c['kind'] for c in cases]} x 5 reps, rates {start}+1000.., seeds {p['seeds']}")
PYV
  for k in $KINDS; do $PY $H/scripts/compare_to_n1.py $R $N1 $k --new-images || fail "$k differs from its n=1 root $N1"; done
  echo $R >> $ROOTS
  [ -n "$DRYRUN" ] && continue
  echo "$(date -u +%H:%M:%S) run hotel real-work $KINDS x 5 reps $R"
  $PY run_dsb_sn_nw.py run --out $R --skip-smoke > $ST/hotelrw_${group}_run.log 2>&1 || fail "run $group (see $ST/hotelrw_${group}_run.log)"
  echo "$(date -u +%H:%M:%S) done hotel real-work $KINDS"
done
[ -n "$DRYRUN" ] && { echo "DRYRUN COMPLETE hotelrw"; exit 0; }
echo "$(date -u +%H:%M:%S) PASSES CHAIN COMPLETE hotelrw real-work n=5 (nt, v, pb cgpb sb no passthrough)"
