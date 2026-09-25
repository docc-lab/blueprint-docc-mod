#!/usr/bin/env python3
"""Tomislav-RetCtx: exact per-trace outcome per point from the SDK refused-trace census (numpy):
intact (no span lost) / reconstructable (only LP lost) / broken (a checkpoint lost; vanilla: any
span lost). Writes census_outcome.json; usage: census_outcome.py label=point_dir ..."""
import glob, json, os, sys
import numpy as np
def load(path):
    b=np.frombuffer(open(path,'rb').read(), dtype=np.uint8)
    n=len(b)//17; b=b[:n*17].reshape(n,17)
    ids=np.ascontiguousarray(b[:,:16]).view(np.uint64).reshape(n,2); flags=b[:,16]
    return ids, flags
def uniq(a): return np.unique(a, axis=0) if len(a) else a.reshape(0,2)
out=json.load(open('census_outcome.json')) if os.path.exists('census_outcome.json') else {}
for arg in sys.argv[1:]:
    label,P=arg.split('=',1)
    res=json.load(open(f'{P}/result.json')); completed=res['completed_requests']; kind=res.get('kind')
    hp=[]; anyl=[]
    for after in sorted(glob.glob(f'{P}/after/refused-*.bin')):
        before=f'{P}/before/refused-{os.path.basename(after)[8:]}'
        nb=os.path.getsize(before)//17 if os.path.exists(before) else 0
        ids,flags=load(after); ids=ids[nb:]; flags=flags[nb:]
        hp.append(ids[(flags&1)>0]); anyl.append(ids[(flags&2)>0])
    hp=uniq(np.concatenate(hp)) if hp else np.zeros((0,2),np.uint64); anyl=uniq(np.concatenate(anyl)) if anyl else np.zeros((0,2),np.uint64)
    n_hp=len(hp); n_any=len(anyl)
    broken = n_any if kind=='v' else n_hp
    recon = 0 if kind=='v' else n_any-n_hp
    intact = completed-n_any
    out[label]=dict(kind=kind, point=P, completed=completed, traces_any_loss=int(n_any), traces_hp_loss=int(n_hp),
                    intact=int(intact), reconstructable=int(recon), broken=int(broken),
                    pct=dict(intact=100*intact/completed, reconstructable=100*recon/completed, broken=100*broken/completed))
    p=out[label]['pct']; print(f"{label:9s} completed {completed:,}  intact {p['intact']:6.2f}%  reconstructable {p['reconstructable']:6.2f}%  broken {p['broken']:6.3f}%   (any-loss {n_any:,}, hp-loss {n_hp:,})")
json.dump(out, open('census_outcome.json','w'), indent=1)
