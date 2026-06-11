#!/usr/bin/env bash
# dump_otelcol_drops.sh — scrape every otelcol pod's /metrics via parallel
# port-forwards, print a table of drop-related counters with a TOTAL row.
#
# OTel collector's prometheus exporter defaults to listening only on
# localhost:8888 inside the pod, so we use kubectl port-forward (which
# tunnels into the pod's loopback) to reach it without any config or
# image changes. Pods are forwarded concurrently on consecutive local
# ports so a 9-pod sweep takes ~1s instead of ~9s.
#
# Usage:
#   dump_otelcol_drops.sh                 # one-shot, all otelcol-*-ctr pods
#   dump_otelcol_drops.sh --watch 1       # repeat every 1s, show deltas
#   dump_otelcol_drops.sh --selector NAME # filter pods to substring match
#
# Counters reported (per OTel collector v0.139 contrib):
#   receiver_refused_spans       receiver rejected the batch entirely
#   receiver_failed_spans        receiver-side pipeline error
#   exporter_send_failed_spans   exporter couldn't deliver to downstream
#   exporter_sent_spans          successful deliveries (sanity column)
#
# Drops at the SDK side (in-process app -> collector) are tracked by the
# vanilla_processor.go counters and visible in `kubectl logs` via the
# `vanilla_processor_metrics` log line.

set -uo pipefail

WATCH_INTERVAL=0
SELECTOR='^otelcol-.*-ctr-'

while [[ $# -gt 0 ]]; do
    case "$1" in
        --watch)    WATCH_INTERVAL="$2"; shift 2;;
        --selector) SELECTOR="$2"; shift 2;;
        -h|--help)
            sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
            exit 0;;
        *) echo "unknown arg: $1" >&2; exit 1;;
    esac
done

METRICS=(
    otelcol_receiver_refused_spans
    otelcol_receiver_failed_spans
    otelcol_exporter_enqueue_failed_spans
    otelcol_exporter_send_failed_spans
    otelcol_exporter_sent_spans
)

# --- discover pods --------------------------------------------------------
PODS=()
while IFS= read -r line; do
    [[ -n "$line" ]] && PODS+=("$line")
done < <(kubectl get pods -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
         | grep -E "$SELECTOR")

if [[ ${#PODS[@]} -eq 0 ]]; then
    echo "no pods matched selector '$SELECTOR'" >&2
    exit 1
fi

# --- spawn one port-forward per pod, concurrently -------------------------
BASE_PORT=18888
PIDS=()
declare -A POD_PORT
for i in "${!PODS[@]}"; do
    port=$((BASE_PORT + i))
    POD_PORT[${PODS[$i]}]=$port
    kubectl port-forward "pod/${PODS[$i]}" "$port:8888" >/dev/null 2>&1 &
    PIDS+=($!)
done
# Give the forwarders a moment to bind.
sleep 1.5

cleanup() {
    for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null; done
    wait 2>/dev/null
}
trap cleanup EXIT INT TERM

# --- counter scrape -------------------------------------------------------
# scrape_metric: prints a single number — the sum of label-time-series for
# a given counter name, taken from the prometheus exposition body passed
# on stdin. Skips _bucket / _sum / _count rows from histograms. Optional
# second arg is a label-substring filter (e.g. 'exporter="otlp"') applied
# to $1 before summing — used to dedupe the per-exporter counters since
# otelcol emits one time series per configured exporter and we'd otherwise
# double-count each span (debug + otlp).
#
# Prom exposition format puts a metric's labelset directly attached to its
# name as one whitespace token, e.g.
#   otelcol_exporter_sent_spans{exporter="otlp",...} 844437
# So $1 looks like "<name>{<labels>}" and we need to match $1 == <name>
# (unlabeled, rare) OR $1 starts with "<name>{". The "{" must go inside a
# character class — bare "{" in an awk regex is a repetition-operator
# trigger and silently fails to match in gawk.
scrape_metric() {
    local name=$1
    local label_filter="${2:-}"
    awk -v m="$name" -v lf="$label_filter" '
        $1 == m || $1 ~ ("^" m "[{]") {
            if ($1 ~ /_bucket|_sum|_count/) next
            if (lf != "" && index($1, lf) == 0) next
            sum += $NF
        }
        END { printf "%d", sum+0 }
    '
}

# scrape_all: prints one TSV row per pod, columns = METRICS in order.
# Per-exporter counters (otelcol_exporter_*) are filtered to the otlp
# exporter only so the debug exporter doesn't double the totals — every
# span passes through both exporters in our pipeline.
scrape_all() {
    for pod in "${PODS[@]}"; do
        body=$(curl -sS -m 3 "http://localhost:${POD_PORT[$pod]}/metrics" 2>/dev/null || true)
        printf '%s' "$pod"
        for m in "${METRICS[@]}"; do
            case "$m" in
                otelcol_exporter_*)
                    v=$(printf '%s' "$body" | scrape_metric "$m" 'exporter="otlp"');;
                *)
                    v=$(printf '%s' "$body" | scrape_metric "$m");;
            esac
            printf '\t%d' "$v"
        done
        printf '\n'
    done
}

# print_table: pretty-prints scrape_all output with a TOTAL row.
print_table() {
    awk -F'\t' -v cols="${#METRICS[@]}" '
        BEGIN {
            split("'"$(IFS='|'; echo "${METRICS[*]}")"'", labels, "|")
            printf "%-44s", "POD"
            for (i=1; i<=cols; i++) {
                gsub("otelcol_", "", labels[i])
                printf "  %22s", labels[i]
            }
            print ""
        }
        {
            printf "%-44s", $1
            for (i=1; i<=cols; i++) {
                printf "  %22d", $(i+1)
                tot[i] += $(i+1)
            }
            print ""
        }
        END {
            sep = ""; for (i=0; i<44+(cols*24); i++) sep = sep "-"
            print sep
            printf "%-44s", "TOTAL"
            for (i=1; i<=cols; i++) printf "  %22d", tot[i]
            print ""
        }
    '
}

# --- run once or watch ----------------------------------------------------
if [[ "$WATCH_INTERVAL" == "0" ]]; then
    scrape_all | print_table
    exit 0
fi

# Watch mode: print a fresh table every WATCH_INTERVAL seconds AND a
# DELTA row computed against the previous tick. Both totals and per-pod
# absolute counters are easy to misread under load; the delta is what
# tells you whether drops are happening RIGHT NOW.
PREV=""
while true; do
    now=$(date +'%H:%M:%S')
    cur=$(scrape_all)
    printf '\n=== %s ===\n' "$now"
    printf '%s\n' "$cur" | print_table
    if [[ -n "$PREV" ]]; then
        echo
        echo "DELTAS (vs previous tick):"
        paste <(printf '%s\n' "$PREV") <(printf '%s\n' "$cur") \
        | awk -F'\t' -v cols="${#METRICS[@]}" '
            {
                printf "%-44s", $1
                for (i=1; i<=cols; i++) {
                    a = $(i+1); b = $(cols+2+i)
                    printf "  %22d", (b - a)
                }
                print ""
            }
        '
    fi
    PREV="$cur"
    sleep "$WATCH_INTERVAL"
done
