#!/bin/bash
# Tomislav-RetCtx: probe per-second sources for vanilla loss + Jaeger queue-full pushback
BV=/storage/tomislav-retctx-e2e/retctx-nwe2e-burst6k-on-20260922T081601Z/run/01-v/rate-06000
FV=/storage/tomislav-retctx-e2e/retctx-nwe2e-fixed5570-on-20260922T094851Z/run/01-v/rate-05570
[ -d "$FV" ] || FV=$(ls -d /storage/tomislav-retctx-e2e/retctx-nwe2e-fixed5570-on-20260922T094851Z/run/01-v/rate-* | head -1)
echo "FV=$FV"
for P in $BV $FV; do
  echo "################ $P"
  echo "### vanilla_processor_metrics cadence per service (count, first ts, last ts)"
  for f in $P/after/logs-*-service-*.txt.gz; do
    n=$(basename $f | sed -E 's/logs-(.*)-service-v-es.*/\1/')
    zcat $f | grep vanilla_processor_metrics | awk -v n=$n 'NR==1{first=$1" "$2} {last=$1" "$2; c++} END{printf "%-14s n=%d first=%s last=%s\n", n, c, first, last}'
  done
  echo "### service log line counts and first/last timestamps (truncation check)"
  for f in $P/after/logs-*-service-*.txt.gz; do
    n=$(basename $f | sed -E 's/logs-(.*)-service-v-es.*/\1/')
    zcat $f | awk -v n=$n 'NR==1{first=$1" "$2} {last=$1" "$2; c++} END{printf "%-14s lines=%d first=%s last=%s\n", n, c, first, last}'
  done
  echo "### 'Failed to send batch' count per service and summed count= field"
  for f in $P/after/logs-*-service-*.txt.gz; do
    n=$(basename $f | sed -E 's/logs-(.*)-service-v-es.*/\1/')
    zcat $f | grep "Failed to send batch" | awk -v n=$n '{for(i=1;i<=NF;i++) if($i ~ /^count=/){split($i,a,"="); s+=a[2]}; c++} END{printf "%-14s batches=%d spans=%d\n", n, c, s}'
  done
  echo "### memorylimiter transitions per collector (refuse/resume counts, first refuse ts)"
  for f in $P/after/logs-otelcol-*.txt.gz; do
    id=$(basename $f | sed -E 's/.*-ctr-(.*)\.txt\.gz/\1/')
    zcat $f | awk -v id=$id '/Refusing data/{r++; if(!fr) fr=$1} /Resuming normal/{s++} /above hard limit/{h++} END{printf "%-6s refuse=%d resume=%d hard=%d first_refuse=%s\n", id, r, s, h, fr}'
  done
  echo "### jaeger 'sending queue is full' retries per collector, within point window"
  st=$(python3 -c "import json;print(json.load(open('$P/command.json'))['started'][:19])")
  fi=$(python3 -c "import json;print(json.load(open('$P/result.json'))['finished'][:19])")
  echo "window $st .. $fi"
  for f in $P/after/logs-otelcol-*.txt.gz; do
    id=$(basename $f | sed -E 's/.*-ctr-(.*)\.txt\.gz/\1/')
    zcat $f | grep "retry_sender" | awk -v id=$id -v st="$st" -v fi="$fi" '{ts=substr($1,1,19); gsub("T"," ",ts); s2=st; gsub("T"," ",s2); f2=fi; gsub("T"," ",f2); if(ts>=s2 && ts<=f2){ if($0 ~ /queue is full/) q++; else o++ }} END{printf "%-6s queue_full=%d other=%d\n", id, q, o}'
  done
  echo "### jaeger backend log tail"
  zcat $P/after/backend-jaeger-*.txt.gz | grep -v -E "^\s*$" | tail -5 | cut -c1-260
  zcat $P/after/logs-jaeger-*.txt.gz | grep -i -E "queue|drop|full" | tail -5 | cut -c1-260
  echo "### per-collector prometheus refused/accepted (after-before)"
  python3 - "$P" <<'PY'
import sys,gzip,re,glob,os
P=sys.argv[1]
def load(d):
    out={}
    for f in glob.glob(f'{d}/prometheus-otelcol-*.txt.gz'):
        cid=re.search(r'-ctr-(\w+)\.txt\.gz',f).group(1)
        m={}
        for line in gzip.open(f,'rt'):
            if line.startswith('#'): continue
            mm=re.match(r'(otelcol_(receiver_accepted_spans|receiver_refused_spans|processor_refused_spans|exporter_sent_spans|exporter_send_failed_spans)\w*)\{?[^}]*\}?\s+([0-9.e+]+)',line)
            if mm:
                k=mm.group(2); m[k]=m.get(k,0)+float(mm.group(3))
        out[cid]=m
    return out
b=load(P+'/before'); a=load(P+'/after')
tot={}
for cid in sorted(a):
    d={k:a[cid].get(k,0)-b.get(cid,{}).get(k,0) for k in a[cid]}
    acc=d.get('receiver_accepted_spans',0); ref=d.get('receiver_refused_spans',0)
    print(f"{cid:6s} accepted={acc:>10.0f} refused={ref:>10.0f} refused%={100*ref/max(1,acc+ref):5.1f} sent={d.get('exporter_sent_spans',0):>10.0f}")
    for k,v in d.items(): tot[k]=tot.get(k,0)+v
print('TOTAL',{k:int(v) for k,v in tot.items()})
PY
done
