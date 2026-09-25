#!/usr/bin/env bash
# Tomislav-RetCtx: rebuild otelcontribcol from contrib HEAD ca8f540b (the pinned image's commit)
# plus the depth_cubic configdiscovery change AND the priority processor us_margin_mode=backlog + lp_refusal_code + cpu_shed_threshold options and the priorityotlp pre-decode receiver (defaults unchanged). Pushed under a
# NEW tag only; :latest and every pinned digest are left untouched.
set -euo pipefail
S=/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad
cd /users/tomislav/opentelemetry-collector-contrib
echo "$(date -u +%T) start; HEAD $(git rev-parse --short HEAD); diff:"; git diff --stat
export GOTOOLCHAIN=go1.24.13
make docker-otelcontribcol
TAG=10.10.1.1:30000/otelcontribcol:prio-predecode-20260923
docker tag otelcontribcol:latest $TAG
docker push $TAG
DIGEST=$(docker image inspect $TAG --format '{{range .RepoDigests}}{{println .}}{{end}}' | grep '^10.10.1.1:30000/otelcontribcol@')
echo "$DIGEST" > $S/collector_predecode_digest
echo "$(date -u +%T) DONE $DIGEST"
