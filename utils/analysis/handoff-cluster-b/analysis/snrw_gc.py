# Tomislav-RetCtx: per-point Go GC activity (gctrace) in the app services, nt vs v: GCs in the window, total STW, heap at GC
import glob, gzip, re, sys, os
D = "/users/tomislav/deployments/dsb-sn"
roots = {"nt": f"{D}/retctx-snrw-final-nt-20260924T163105Z", "v": f"{D}/retctx-snrw-final-v-20260924T163105Z"}
GC = re.compile(r"gc (\d+) @([\d.]+)s (\d+)%: ([\d.]+)\+([\d.]+)\+([\d.]+) ms clock.* (\d+)->(\d+)->(\d+) MB")
def gcs(path):
    try: return {int(m.group(1)): m for m in GC.finditer(gzip.open(path, "rt", errors="replace").read())}
    except FileNotFoundError: return {}
rates = [int(r) for r in sys.argv[1:]] or [2300, 2800]
for rate in rates:
    for k, R in roots.items():
        tot_n, tot_stw, worst, svc_n = 0, 0.0, 0.0, {}
        for c in sorted(glob.glob(f"{R}/run/0?-{k}")):
            P = f"{c}/rate-{rate:05d}"
            for a in glob.glob(f"{P}/after/logs-*-service-*.txt.gz"):
                s = os.path.basename(a).split("-service")[0][5:]
                aft, bef = gcs(a), gcs(a.replace("/after/", "/before/"))
                new = [m for n, m in aft.items() if n not in bef]
                svc_n[s] = svc_n.get(s, 0) + len(new)
                for m in new:
                    stw = float(m.group(4)) + float(m.group(6)); tot_stw += stw; worst = max(worst, stw); tot_n += 1
        top = sorted(svc_n.items(), key=lambda x: -x[1])[:4]
        print(f"{rate} {k:2s}: {tot_n/5:6.1f} GCs/window (all services, mean of 5 reps), STW total {tot_stw/5:6.2f} ms, worst STW {worst:.2f} ms | top: {top}")
