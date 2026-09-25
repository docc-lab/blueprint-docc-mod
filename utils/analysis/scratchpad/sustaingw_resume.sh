#!/usr/bin/env bash
# Tomislav-RetCtx: resume the capped-gateway sweep (v completed by hand; runner skips it and runs cgpb)
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
log() { echo "$(date -u +%H:%M:%S) $*" >> $S/sustaingw.log; }
log "RESUME run (cgpb) after settle fix"
sg docker -c "cd /users/tomislav/blueprint-docc-mod && RETCTX_TRACE_SAMPLE_WINDOWS=10 .venv/bin/python -B -u utils/run_dsb_sn_nw.py run --out /users/tomislav/deployments/dsb-sn/retctx-nwe2e-sustaingw-on-20260922T194711Z" >"/users/tomislav/deployments/dsb-sn/retctx-nwe2e-sustaingw-on-20260922T194711Z/logs/run-resume.log" 2>&1 || { log "RUN FAILED (resume)"; exit 1; }
log "SUSTAINGW COMPLETE"
