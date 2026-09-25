#!/usr/bin/env bash
# Tomislav-RetCtx: put the git-TRACKED build dirs back (a symlink in place of a tracked dir reads as deleted files to git).
set -uo pipefail
cd /users/tomislav/blueprint-docc-mod
A=/storage/tomislav-archive
for app in dsb_sn dsb_hotel; do
  for d in $(git ls-files examples/$app | grep "^examples/$app/build_" | cut -d/ -f3 | sort -u); do
    p=examples/$app/$d
    if [ -L "$p" ] && [ -d "$A/examples/$app/$d" ]; then
      rm "$p" && mv "$A/examples/$app/$d" "$p" && echo "restored $p" || echo "FAILED $p"
    fi
  done
done
df -h / | tail -1
echo RESTORE DONE
