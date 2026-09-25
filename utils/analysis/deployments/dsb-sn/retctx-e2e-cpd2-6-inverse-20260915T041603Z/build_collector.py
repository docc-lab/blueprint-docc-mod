from pathlib import Path
from datetime import datetime,timezone
import json,subprocess,os
root=Path(__file__).resolve().parent
def status(state,**extra):
    p=root/'collector-build-status.json'
    temporary=p.with_suffix('.tmp')
    temporary.write_text(json.dumps({'state':state,'updated':datetime.now(timezone.utc).isoformat(),**extra},indent=2)+'\n')
    temporary.replace(p)
status('running')
result=subprocess.run(['./build-and-push.sh','10.10.1.1:30000'],cwd='/users/tomislav/opentelemetry-collector-contrib',env=dict(os.environ,GOTOOLCHAIN='go1.24.13'))
status('complete' if result.returncode==0 else 'failed',returncode=result.returncode)
raise SystemExit(result.returncode)
