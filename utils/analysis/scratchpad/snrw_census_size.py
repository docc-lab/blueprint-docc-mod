import json, glob, gzip, re, os
roots = [l.strip() for l in open("/storage/retctx-handoff/state/snrw_roots.txt") if l.strip()]
for R in roots:
    for case in sorted(glob.glob(R + "/run/01-*")):
        kind = case.rsplit("-", 1)[1]
        pts = sorted(glob.glob(case + "/rate-*"))
        if not pts:
            continue
        last = pts[-1]
        snap = json.load(open(last + "/after/snapshot.json"))
        ref = snap.get("refused", {})
        tr_any = sum(v.get("traces_any", 0) for v in ref.values())
        sp = sum(v.get("spans_all", 0) for v in ref.values())
        heaps = []
        for f in glob.glob(last + "/after/logs-*.txt.gz"):
            lines = [l for l in gzip.open(f, "rt", errors="replace") if l.startswith("gc ")]
            if lines:
                m = re.search(r"(\d+)->(\d+)->(\d+) MB", lines[-1])
                if m:
                    heaps.append((int(m.group(3)), os.path.basename(f).split("-")[1]))
        heaps.sort(reverse=True)
        print("%-5s %s: SDK-refused traces (all services, whole ramp) %s, spans %s; largest live heaps %s" % (
            kind, os.path.basename(last), format(tr_any, ","), format(sp, ","), heaps[:3]))
