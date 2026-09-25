import sys,json
from pathlib import Path
sys.path.insert(0, '/users/tomislav/blueprint-docc-mod/utils')
from run_dsb_sn_e2e import settle_trace_samples,snapshot,now
from prepare_dsb_sn_e2e import write_json
root=Path(__file__).resolve().parent
case=json.loads((root/'cases.json').read_text())[0]
try:
 write_json(root/'interrupted-capture-status.json',{'state':'running','started':now()})
 settle_trace_samples(case,root/'run/01-v')
 snapshot(case['namespace'],case['variant'],root/'run/01-v/interrupted-final-rechecked')
 write_json(root/'interrupted-capture-status.json',{'state':'complete','finished':now()})
except Exception as e:
 write_json(root/'interrupted-capture-status.json',{'state':'failed','error':str(e),'finished':now()})
 raise
