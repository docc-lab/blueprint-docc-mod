#!/usr/bin/env bash
# One-shot Hotel Reservation build/deploy; see --help for options.
set -e
HOTEL_REPO="$(cd "$(dirname "$0")/.." && pwd)"

# Load the same build environment as interactive work in this repository.
if [[ -f "$HOME/.profile" ]]; then
  source "$HOME/.profile"
fi
if [[ ! -f "$HOTEL_REPO/.venv/bin/activate" ]]; then
  echo "Missing $HOTEL_REPO/.venv; run utils/setup_environment.sh first." >&2
  exit 1
fi
source "$HOTEL_REPO/.venv/bin/activate"
exec python "$HOTEL_REPO/utils/build_deploy_hotel.py" "$@"
