# Tomislav-RetCtx: SN real-work nt vs v, pooled p99 (merged wrk2 HdrHistogram spectra) + bootstrap 5-95 % band over
# repetitions, request-weighted mean; flags rates whose bands do not overlap
import glob, json, random, sys
sys.path.insert(0, "/users/tomislav/blueprint-docc-mod/utils")
from pool_latency import pooled
D = "/users/tomislav/deployments/dsb-sn"
roots = {"nt": f"{D}/retctx-snrw-final-nt-20260924T163105Z", "v": f"{D}/retctx-snrw-final-v-20260924T163105Z"}
random.seed(1)
def stats(k, rate):
    P = sorted(glob.glob(f"{roots[k]}/run/0?-{k}/rate-{rate:05d}"))
    res = [json.load(open(f"{p}/result.json")) for p in P]
    w = [r["completed_requests"] for r in res]
    mean = sum(r["mean_ms"] * n for r, n in zip(res, w)) / sum(w)
    p99 = pooled([f"{p}/wrk.stdout" for p in P])[0.99]["pooled"]
    boot = sorted(pooled([f"{random.choice(P)}/wrk.stdout" for _ in P])[0.99]["pooled"] for _ in range(400))
    return sum(r["completed_rps"] for r in res) / len(res), mean, p99, boot[20], boot[379]
print("rate |  nt got   mean  p99 [5-95 band]      |  v got   mean  p99 [5-95 band]      | bands")
for rate in range(2000, 3601, 100):
    a, b = stats("nt", rate), stats("v", rate)
    sep = "nt worse" if a[3] > b[4] else ("v worse" if b[3] > a[4] else "overlap")
    print(f"{rate} | {a[0]:6.0f} {a[1]:6.1f} {a[2]:6.1f} [{a[3]:6.1f},{a[4]:6.1f}] | {b[0]:6.0f} {b[1]:6.1f} {b[2]:6.1f} [{b[3]:6.1f},{b[4]:6.1f}] | {sep}")
