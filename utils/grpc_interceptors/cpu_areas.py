#!/usr/bin/env python3
"""Where does the CPU go?  Splits CPU profiles into areas (tracing interceptors, OTel SDK, bridge
processor, gRPC, HTTP/2 header compression, GC, ...) and prints the seconds in each, for one set
of profiles or two side by side.

Usage:
    cpu_areas.py <dir A> [<dir B>]

    dir   a directory of pprof CPU profiles, e.g. <run>/pprof from run_local.sh with
          PROFILE_SECONDS (all *_proc.pb.gz in it are merged; allocs profiles are skipped)

The areas overlap where one calls the other (e.g. "span start" includes the processor's OnStart,
listed again on its own); each row is the CPU of all samples whose stack matches the row's pattern
(go tool pprof -focus).  "interceptor's own code" is the interceptors minus everything they call
that is listed elsewhere (the handler or the downstream call, span start and end, the checkpoint
preparation).

Needs `go` on PATH.

Example:
    utils/grpc_interceptors/cpu_areas.py $W/prof/pb_final2/wrapper/pprof $W/prof/pb_final2/interceptor/pprof
"""
import glob
import os
import re
import subprocess
import sys
import tempfile

CALLEES = (r'_Handler\.func1|grpc\.invoke$|sdk/trace\.\(\*tracer\)\.Start|recordingSpan\)\.End|'
           r'PrepareCheckpoint|ot_server_wrapper|ot_client_wrapper|OTServerWrapper|OTClientWrapper')

# (label, focus regex, ignore regex or None)
AREAS = [
    ('everything', '.', None),
    ('tracing: interceptor\'s own code (server)', r'interceptors\.serverInterceptor\.func1', CALLEES),
    ('tracing: interceptor\'s own code (client)', r'interceptors\.clientInterceptor\.func1', CALLEES),
    ('tracing: span start (SDK + processor OnStart)', r'sdk/trace\.\(\*tracer\)\.Start', None),
    ('   of which processor OnStart', r'Processor\)\.OnStart', None),
    ('tracing: span end (SDK + processor OnEnd)', r'sdk/trace\.\(\*recordingSpan\)\.End', None),
    ('   of which processor OnEnd', r'Processor\)\.OnEnd', None),
    ('tracing: checkpoint preparation (reverse path)', r'PrepareCheckpoint', None),
    ('tracing: export to the collector (background)', r'exportBatch|flushBuffer|Processor\)\.sendData', None),
    ('gRPC: HTTP/2 header compression (hpack)', r'http2/hpack', None),
    ('gRPC: transport goroutines (loopy writer, reader)', r'loopyWriter|http2Client\)\.reader|http2Server\)\.HandleStreams', None),
    ('gRPC: protobuf marshal/unmarshal', r'google\.golang\.org/protobuf', None),
    ('application handlers (workflow code)', r'/workflow/', None),
    ('runtime: garbage collection', r'runtime\.gcBgMarkWorker|runtime\.gcAssistAlloc|runtime\.bgsweep|runtime\.bgscavenge', None),
    ('runtime: allocation (mallocgc)', r'runtime\.mallocgc', None),
    ('runtime: stack growth', r'runtime\.newstack', None),
    ('runtime: scheduler', r'runtime\.schedule$|runtime\.findRunnable|runtime\.park_m|runtime\.goschedImpl', None),
    ('runtime: context lookups (context.value)', r'^context\.value$', None),
    ('syscalls (network I/O, futex)', r'syscall\.Syscall|internal/runtime/syscall|runtime\.futex', None),
]


def samples(profile):
    """All sample stacks of a profile as (seconds, frames), from go tool pprof -traces."""
    out = subprocess.run(['go', 'tool', 'pprof', '-traces', profile], capture_output=True, text=True, check=True).stdout
    result = []
    for block in out.split('-----------+-------------------------------------------------------'):
        lines = [l.strip() for l in block.strip('\n').split('\n') if l.strip()]
        if not lines:
            continue
        m = re.match(r'([\d.]+)(us|ms|s)\s+(.*)', lines[0])
        if not m:
            continue
        t = float(m.group(1)) * {'us': 1e-6, 'ms': 1e-3, 's': 1.0}[m.group(2)]
        result.append((t, [m.group(3)] + lines[1:]))
    return result


def seconds(stacks, focus, ignore):
    """CPU of the stacks with a frame matching focus and none matching ignore (pprof's -focus and
    -ignore)."""
    f = re.compile(focus)
    i = re.compile(ignore) if ignore else None
    return sum(t for t, frames in stacks
               if any(f.search(fr) for fr in frames) and not (i and any(i.search(fr) for fr in frames)))


def merged(profiles, tmp):
    """Merge the profiles into one file once (go tool pprof -proto), so each area query reads one
    profile instead of merging all of them again."""
    out = os.path.join(tmp, f'merged{len(os.listdir(tmp))}.pb.gz')
    with open(out, 'wb') as f:
        subprocess.run(['go', 'tool', 'pprof', '-proto', *profiles], stdout=f, stderr=subprocess.DEVNULL, check=True)
    return [out]


def main():
    if len(sys.argv) not in (2, 3):
        sys.exit(__doc__)
    tmp = tempfile.mkdtemp(prefix='cpu_areas_')
    sets = []
    for d in sys.argv[1:]:
        profiles = sorted(p for p in glob.glob(os.path.join(d, '*.pb.gz')) if not p.endswith('.allocs.pb.gz'))
        if not profiles:
            sys.exit(f'no profiles in {d}')
        sets.append(samples(merged(profiles, tmp)[0]))
    names = [os.path.basename(os.path.dirname(os.path.abspath(d))) or d for d in sys.argv[1:]]
    head = f'{"area":52s}' + ''.join(f'{n[:14]:>16s}' for n in names)
    if len(sets) == 2:
        head += f'{"diff":>10s}'
    print(head, flush=True)
    totals = None
    for label, focus, ignore in AREAS:
        vals = [seconds(p, focus, ignore) for p in sets]
        if totals is None:
            totals = vals
        cells = ''.join(f'{v:9.2f}s {v / t:5.1%}' for v, t in zip(vals, totals))
        line = f'{label:52s}{cells}'
        if len(vals) == 2:
            line += f'{vals[1] - vals[0]:+9.2f}s'
        print(line, flush=True)


if __name__ == '__main__':
    main()
