#!/usr/bin/env bash
# Tomislav-RetCtx: free the root fs (user-approved 2026-09-24): move old Blueprint build outputs (images already in the
# registry) to /storage and leave symlinks so every path still resolves; move Sept-10 /tmp build leftovers. Nothing is deleted.
set -uo pipefail
A=/storage/tomislav-archive
for app in dsb_sn dsb_hotel; do
  mkdir -p $A/examples/$app
  for d in /users/tomislav/blueprint-docc-mod/examples/$app/build_*; do
    [ -L "$d" ] && continue
    case "$d" in *20260924*) continue;; esac
    n=$(basename $d)
    if mv "$d" "$A/examples/$app/$n"; then ln -s "$A/examples/$app/$n" "$d"; echo "moved $app/$n"; else echo "FAILED $d"; fi
  done
done
mkdir -p $A/tmp
for d in /tmp/hotel-check-YVSTJt /tmp/leaf-deep8-HdrKrg /tmp/leaf-fanout-build-tI6TMk /tmp/leaf-patterns-f3UKQe /tmp/releases; do
  [ -e "$d" ] && { mv "$d" $A/tmp/ && echo "moved $d" || echo "FAILED $d"; }
done
df -h / | tail -1
echo ARCHIVE MOVE DONE
