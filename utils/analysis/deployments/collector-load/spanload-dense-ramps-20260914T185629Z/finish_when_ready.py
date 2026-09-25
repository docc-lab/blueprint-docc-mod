#!/usr/bin/env python3
"""Tomislav-RetCtx: complete the audit/figures even if the interactive connection ends."""
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import sys
import time

root = Path(__file__).resolve().parent
def save(state, **extra):
    (root / 'analysis-status.json').write_text(json.dumps({'state': state, 'updated': datetime.now(timezone.utc).isoformat(), **extra}, indent=2) + '\n')

save('waiting_for_measurements')
while not (root / 'completion.json').exists():
    status = json.loads((root / 'status.json').read_text())
    if status['state'] in ('failed', 'interrupted'):
        save('blocked_by_measurement_failure', detail=status)
        sys.exit(1)
    time.sleep(5)
save('auditing')
with (root / 'analysis.log').open('w') as log:
    result = subprocess.run([sys.executable, str(root / 'analyze_spanload_suite.py'), '--out', str(root)], stdout=log, stderr=subprocess.STDOUT)
save('complete' if result.returncode == 0 else 'failed', returncode=result.returncode)
sys.exit(result.returncode)
