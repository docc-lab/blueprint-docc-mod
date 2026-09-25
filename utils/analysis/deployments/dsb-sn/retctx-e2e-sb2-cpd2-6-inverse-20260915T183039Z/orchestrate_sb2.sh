#!/bin/bash
# Tomislav-RetCtx: sequential driver for the rebuilt S-Bridge: prepare/build both regimes, then no-work smoke+run, then real-work smoke+run.
set -uo pipefail
source ~/.profile 2>/dev/null; source /users/tomislav/blueprint-docc-mod/.venv/bin/activate; cd /users/tomislav/blueprint-docc-mod
NW=/users/tomislav/deployments/dsb-sn/retctx-nw-sb2-cpd2-6-inverse-20260915T183039Z; E2E=/users/tomislav/deployments/dsb-sn/retctx-e2e-sb2-cpd2-6-inverse-20260915T183039Z; OLDE2E=/users/tomislav/deployments/dsb-sn/retctx-e2e-cpd2-6-inverse-20260915T041603Z; COL=10.10.1.1:30000/otelcontribcol@sha256:c16141e6a3d1f1a0276b63d02228667b55bff6c93100e89cc23a770e71ae9181
status() { printf '{"step": "%s", "state": "%s", "time": "%s"}\n' "$1" "$2" "$(date -u +%FT%TZ)" > $NW/orchestrator-status.json; cp $NW/orchestrator-status.json $E2E/orchestrator-status.json; echo "[$(date -u +%T)] $1 $2"; }
fail() { status "$1" failed; exit 1; }
status prepare-nw running;  python -B utils/prepare_dsb_sn_nw.py --out $NW --collector-image $COL --kinds sb > $NW/logs/prepare.log 2>&1 || fail prepare-nw
status prepare-e2e running; python -B utils/prepare_dsb_sn_e2e.py --out $E2E --kinds sb > $E2E/logs/prepare.log 2>&1 || fail prepare-e2e
python -c "import json,sys; c=json.load(open('$E2E/cases.json'))[0]['collector_image']; sys.exit(0 if c=='$COL' else 1)" || fail collector-digest-mismatch
status build-nw running;  python -B utils/build_dsb_sn_nw.py --out $NW --backend-manifest $OLDE2E/builds/pb/manifest.yaml > $NW/logs/build.log 2>&1 || fail build-nw
status build-e2e running; python -B utils/build_dsb_sn_e2e.py --out $E2E > $E2E/logs/build.log 2>&1 || fail build-e2e
status smoke-nw running;  python -B -u utils/run_dsb_sn_nw.py smoke --out $NW > $NW/logs/smoke.log 2>&1 || fail smoke-nw
status run-nw running
python -B -u utils/run_dsb_sn_nw.py run --out $NW > $NW/logs/run.log 2>&1 & echo $! > $NW/run.pid
sleep 15; python -B -u $NW/monitor_nw.py > $NW/logs/monitor.log 2>&1 & echo $! > $NW/monitor.pid
wait $(cat $NW/run.pid) || fail run-nw
status smoke-e2e running; python -B -u utils/run_dsb_sn_e2e.py smoke --out $E2E > $E2E/logs/smoke.log 2>&1 || fail smoke-e2e
status run-e2e running
python -B -u utils/run_dsb_sn_e2e.py run --out $E2E > $E2E/logs/run.log 2>&1 & echo $! > $E2E/run.pid
sleep 15; python -B -u $E2E/monitor_e2e.py > $E2E/logs/monitor.log 2>&1 & echo $! > $E2E/monitor.pid
wait $(cat $E2E/run.pid) || fail run-e2e
status all complete
