#!/usr/bin/env python3
"""Tomislav-RetCtx: robust latency across repeat windows of one load level.

Each wrk2 run (-L) writes its HdrHistogram "Detailed Percentile spectrum" (~100 rows of value / percentile /
total count, dense in the tail) to wrk.stdout. Pooling merges the windows' distributions: the pooled CDF at x is
sum_i N_i * F_i(x) / sum_i N_i, with F_i interpolated from window i's spectrum, and a pooled quantile is the
smallest x whose pooled CDF reaches q. Also reports the median / min / max of the per-window quantiles.
usage (CLI): pool_latency.py wrk.stdout [wrk.stdout ...]"""
import bisect, re, statistics, sys

def spectrum(path):
    """[(value_ms, cumulative_fraction)], total count."""
    rows, total, on = [], 0, False
    for line in open(path):
        if 'Detailed Percentile spectrum' in line:
            on = True; continue
        if on:
            m = re.match(r'\s*([\d.]+)\s+([\d.]+)\s+(\d+)\s', line)
            if m:
                rows.append((float(m.group(1)), float(m.group(2)))); total = max(total, int(m.group(3)))
            elif line.startswith('#[Mean'):
                break
    return rows, total

def cdf(rows, x):
    """Fraction of samples <= x (linear interpolation between spectrum rows)."""
    if not rows or x < rows[0][0]: return 0.0
    if x >= rows[-1][0]: return 1.0
    vals = [v for v, _ in rows]; i = bisect.bisect_right(vals, x)
    (v0, p0), (v1, p1) = rows[i - 1], rows[i]
    return p0 if v1 == v0 else p0 + (p1 - p0) * (x - v0) / (v1 - v0)

def quantile(rows, q):
    for (v0, p0), (v1, p1) in zip(rows, rows[1:]):
        if p1 >= q: return v0 if p1 == p0 else v0 + (v1 - v0) * (q - p0) / (p1 - p0)
    return rows[-1][0]

def pooled(paths, qs=(0.5, 0.99)):
    wins = [spectrum(p) for p in paths]; wins = [(r, n) for r, n in wins if r and n]
    total = sum(n for _, n in wins); grid = sorted({v for r, _ in wins for v, _ in r})
    out = {}
    for q in qs:
        lo = next((x for x in grid if sum(n * cdf(r, x) for r, n in wins) / total >= q), grid[-1])
        per = [quantile(r, q) for r, _ in wins]
        out[q] = {'pooled': lo, 'median': statistics.median(per), 'min': min(per), 'max': max(per), 'windows': len(per)}
    return out

if __name__ == '__main__':
    for q, v in pooled(sys.argv[1:]).items():
        print(f"p{q*100:g}: pooled {v['pooled']:.1f} ms | per-window median {v['median']:.1f} min {v['min']:.1f} max {v['max']:.1f} (n={v['windows']})")
