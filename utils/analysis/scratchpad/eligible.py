#!/usr/bin/env python3
"""Tomislav-RetCtx: reconstruction ELIGIBILITY per ramp point, from the wire only.

No reconstruction is run and no ground truth is used. A trace is eligible iff
every window anchor named by a surviving payload is itself present -- i.e. every
surviving fragment still has the checkpoint it would anchor to. A checkpoint
recovered from a returned truss (`_rc`) counts as present, which is exactly what
the response path buys.

Vanilla has no anchors and no recovery, so its counterpart is "no span lost".
"""
import base64, collections, glob, gzip, json, statistics, sys

def uvarint(b, i=0):
    r = s = 0
    while True:
        c = b[i]; i += 1; r |= (c & 0x7f) << s
        if c < 0x80:
            return r, i
        s += 7

def tag(span, key):
    for t in span['tags']:
        if t['key'] == key:
            return t
    return None

def anchors_and_recovered(trace):
    """-> (anchors named by surviving payloads, origin ids restored by a truss)."""
    anchors, recovered = set(), set()
    for s in trace['spans']:
        br = tag(s, '_br')
        if br:
            raw = base64.b64decode(br['value'])
            _, i = uvarint(raw)
            a = raw[i:i + 8]
            if any(a):
                anchors.add(a.hex())
        rc = tag(s, '_rc')
        if not rc:
            continue
        v = rc['value']
        raw = base64.urlsafe_b64decode(v + '=' * (-len(v) % 4))
        if not raw or raw[0] != 1:
            continue
        i = 1
        while i < len(raw):
            t = raw[i]; i += 1
            ln, i = uvarint(raw, i)
            body = raw[i:i + ln]; i += ln
            j = 1 if t & 0x08 else 0
            recovered.add(body[j:j + 8].hex())
            d, j2 = uvarint(body, j + 8)
            truss = body[j + 8 + (j2 - (j + 8)):]
            if len(truss) >= 9:
                _, k = uvarint(truss)
                a = truss[k:k + 8]
                if any(a):
                    anchors.add(a.hex())
    return anchors, recovered

def main():
    S = '/tmp/claude-20002/-users-tomislav/6693a286-50fe-4f08-88f4-79a32690db39/scratchpad'
    roots = [l.strip() for l in open(f'{S}/matrix_roots.txt')]
    out = collections.defaultdict(list)
    for R in roots:
        arm = 'on' if '-m5-on-' in R else 'off'
        for case in sorted(glob.glob(f'{R}/run/01-*')):
            kind = case.rsplit('-', 1)[1]
            if kind == 'nt':
                continue
            for pt in sorted(glob.glob(f'{case}/rate-*')):
                rate = int(pt.rsplit('-', 1)[1])
                try:
                    data = json.load(gzip.open(f'{pt}/settled-traces.json.gz'))['data']
                except OSError:
                    continue
                elig = 0
                for t in data:
                    present = {s['spanID'] for s in t['spans']}
                    if kind == 'v':
                        elig += len(t['spans']) == 23
                        continue
                    anchors, recovered = anchors_and_recovered(t)
                    elig += not (anchors - present - recovered)
                if data:
                    out[(kind, '' if kind == 'v' else arm, rate)].append(100 * elig / len(data))
    json.dump({f'{k[0]}|{k[1]}|{k[2]}': v for k, v in out.items()},
              open(f'{S}/eligible.json', 'w'))
    cols = [('v', ''), ('pb', 'off'), ('pb', 'on'), ('cgpb', 'off'),
            ('cgpb', 'on'), ('sb', 'off'), ('sb', 'on')]
    names = ['vanilla', 'PB off', 'PB on', 'CGPB off', 'CGPB on', 'SB off', 'SB on']
    print('RECONSTRUCTION-ELIGIBLE TRACES (%)  -- mean over 5 rounds, wire-only')
    print(f"{'rate':>6} " + ' '.join(f'{n:>9}' for n in names))
    for rate in sorted({k[2] for k in out}):
        cells = []
        for c in cols:
            v = out.get((c[0], c[1], rate))
            cells.append(f'{statistics.mean(v):9.1f}' if v else f'{"--":>9}')
        print(f'{rate:6} ' + ' '.join(cells))

main()
