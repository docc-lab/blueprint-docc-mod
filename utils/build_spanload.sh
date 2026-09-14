#!/usr/bin/env bash
# Tomislav-RetCtx: build the standalone generator; image publishing is explicit.
set -eo pipefail
SPANLOAD_REPO=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ -f "$HOME/.profile" ]]; then source "$HOME/.profile"; fi
if [[ -f "$SPANLOAD_REPO/.venv/bin/activate" ]]; then source "$SPANLOAD_REPO/.venv/bin/activate"; fi
set -u
SPANLOAD_OUTPUT="$SPANLOAD_REPO/utils/spanload/bin/spanload"
SPANLOAD_IMAGE=""
SPANLOAD_PUSH=0
while (($#)); do
  case "$1" in
    --output) SPANLOAD_OUTPUT=${2:?--output needs a path}; shift 2 ;;
    --image) SPANLOAD_IMAGE=${2:?--image needs a tag}; shift 2 ;;
    --push) SPANLOAD_PUSH=1; shift ;;
    -h|--help) echo 'Usage: utils/build_spanload.sh [--output PATH] [--image REGISTRY/spanload:TAG [--push]]'; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done
if ((SPANLOAD_PUSH)) && [[ -z "$SPANLOAD_IMAGE" ]]; then echo '--push requires --image' >&2; exit 2; fi
mkdir -p -- "$(dirname -- "$SPANLOAD_OUTPUT")"
SPANLOAD_OUTPUT=$(realpath -m -- "$SPANLOAD_OUTPUT")
SPANLOAD_REV=$(git -C "$SPANLOAD_REPO" rev-parse --short=12 HEAD)
if [[ -n $(git -C "$SPANLOAD_REPO" status --porcelain -- utils/spanload utils/build_spanload.sh) ]]; then SPANLOAD_REV+='+dirty'; fi
(
  cd "$SPANLOAD_REPO/utils/spanload"
  CGO_ENABLED=0 GOWORK=off GOTOOLCHAIN="${GOTOOLCHAIN:-go1.24.13}" go build -trimpath -ldflags "-s -w -X main.version=$SPANLOAD_REV" -o "$SPANLOAD_OUTPUT" .
)
echo "Built $SPANLOAD_OUTPUT ($SPANLOAD_REV)" >&2
if [[ -n "$SPANLOAD_IMAGE" ]]; then
  SPANLOAD_CONTEXT=$(mktemp -d /tmp/spanload-image.XXXXXX)
  trap 'rm -rf -- "$SPANLOAD_CONTEXT"' EXIT
  cp -- "$SPANLOAD_OUTPUT" "$SPANLOAD_CONTEXT/spanload"
  cp -- /etc/ssl/certs/ca-certificates.crt "$SPANLOAD_CONTEXT/ca-certificates.crt"
  cp -- "$SPANLOAD_REPO/utils/spanload/Dockerfile" "$SPANLOAD_CONTEXT/Dockerfile"
  docker build -t "$SPANLOAD_IMAGE" "$SPANLOAD_CONTEXT"
  if ((SPANLOAD_PUSH)); then docker push "$SPANLOAD_IMAGE"; fi
fi
