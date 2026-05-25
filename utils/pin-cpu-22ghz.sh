#!/usr/bin/env bash
# pin-cpu-22ghz.sh — pin every logical CPU at the c220g5 base clock
# (2.20 GHz) to control runtime variability across experimental runs.
# Matches bridges.pdf §5.1: "we fix the CPU speed on all nodes at 2.2 GHz".
#
# Mechanism (intel_pstate driver, active mode):
#   1. intel_pstate/no_turbo            = 1          → caps max at base clock
#   2. intel_pstate/max_perf_pct        = 100        → allow up to the cap
#   3. intel_pstate/min_perf_pct        = 100        → forbid throttle below cap
#   4. intel_pstate/hwp_dynamic_boost   = 0          → defeat HWP boost
#   5. cpu*/cpufreq/scaling_governor    = performance → never select a lower P-state
#
# Usage:
#   pin-cpu-22ghz.sh pin              # apply on this host (auto sudo)
#   pin-cpu-22ghz.sh check            # print current state (read-only)
#   pin-cpu-22ghz.sh persist          # apply + install systemd unit (survives reboot)
#   pin-cpu-22ghz.sh unpin            # revert (powersave + no_turbo=0 + disable unit)
#   pin-cpu-22ghz.sh <cmd> --all      # run <cmd> on every k8s node via a one-shot privileged pod
#
# Multi-node mode uses kubectl (no SSH bootstrap needed) — schedules a
# busybox pod with privileged hostPath access to /sys and /proc on each
# node, runs the body, captures logs, deletes the pod. `persist --all`
# is not supported (would need systemd nsenter); install via per-node
# local invocation instead.

set -euo pipefail

THIS=$(readlink -f "$0")
UNIT=/etc/systemd/system/cpu-pin-22ghz.service
POD_LABEL=pin-cpu-22ghz
POD_IMAGE=busybox:1.36

usage() {
    sed -n '2,/^$/p' "$THIS" | sed 's/^# \{0,1\}//'
    exit "${1:-0}"
}

# ------------- local execution (paths under /sys directly) -------------

cmd_check() {
    local mean_mhz
    mean_mhz=$(awk '/^cpu MHz/ {s+=$4; n++} END {if (n) printf "%.0f", s/n}' /proc/cpuinfo)
    printf 'host=%-12s gov=%-12s no_turbo=%s perf_pct=%s/%s hwp_boost=%s mean_freq=%s MHz\n' \
        "$(hostname)" \
        "$(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)" \
        "$(cat /sys/devices/system/cpu/intel_pstate/no_turbo)" \
        "$(cat /sys/devices/system/cpu/intel_pstate/min_perf_pct)" \
        "$(cat /sys/devices/system/cpu/intel_pstate/max_perf_pct)" \
        "$(cat /sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost 2>/dev/null || echo n/a)" \
        "$mean_mhz"
}

apply_pin() {
    local drv
    drv=$(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_driver)
    if [[ "$drv" != "intel_pstate" ]]; then
        echo "ERROR: scaling_driver='$drv' (expected intel_pstate)" >&2
        exit 2
    fi
    echo 1   > /sys/devices/system/cpu/intel_pstate/no_turbo
    echo 100 > /sys/devices/system/cpu/intel_pstate/max_perf_pct
    echo 100 > /sys/devices/system/cpu/intel_pstate/min_perf_pct
    if [[ -w /sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost ]]; then
        echo 0 > /sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost
    fi
    for g in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
        echo performance > "$g"
    done
    sleep 0.4
}

cmd_pin()   { apply_pin; cmd_check; }

cmd_unpin() {
    echo 0 > /sys/devices/system/cpu/intel_pstate/no_turbo
    for g in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
        echo powersave > "$g"
    done
    if systemctl list-unit-files cpu-pin-22ghz.service >/dev/null 2>&1; then
        systemctl disable --now cpu-pin-22ghz.service 2>/dev/null || true
    fi
    sleep 0.4
    cmd_check
}

cmd_persist() {
    apply_pin
    cat >"$UNIT" <<'EOF'
[Unit]
Description=Pin CPUs at base clock (2.2 GHz) for bridges experiments
After=multi-user.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/bash -c '\
echo 1   > /sys/devices/system/cpu/intel_pstate/no_turbo; \
echo 100 > /sys/devices/system/cpu/intel_pstate/max_perf_pct; \
echo 100 > /sys/devices/system/cpu/intel_pstate/min_perf_pct; \
[ -w /sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost ] && \
    echo 0 > /sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost; \
for g in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do \
    echo performance > "$g"; \
done'

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable --now cpu-pin-22ghz.service
    cmd_check
}

# ------------- remote execution bodies (paths under /host/sys, /host/proc) -------------

REMOTE_CHECK_SH='
host=/host
mean=$(awk "/^cpu MHz/{s+=\$4;n++}END{if(n)printf \"%.0f\",s/n}" "$host/proc/cpuinfo")
printf "gov=%-12s no_turbo=%s perf_pct=%s/%s hwp_boost=%s mean_freq=%s MHz\n" \
    "$(cat "$host"/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor)" \
    "$(cat "$host"/sys/devices/system/cpu/intel_pstate/no_turbo)" \
    "$(cat "$host"/sys/devices/system/cpu/intel_pstate/min_perf_pct)" \
    "$(cat "$host"/sys/devices/system/cpu/intel_pstate/max_perf_pct)" \
    "$(cat "$host"/sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost 2>/dev/null || echo n/a)" \
    "$mean"
'

REMOTE_PIN_SH='
host=/host
drv=$(cat "$host"/sys/devices/system/cpu/cpu0/cpufreq/scaling_driver)
if [ "$drv" != "intel_pstate" ]; then echo "ERROR: scaling_driver=$drv" >&2; exit 2; fi
echo 1   > "$host"/sys/devices/system/cpu/intel_pstate/no_turbo
echo 100 > "$host"/sys/devices/system/cpu/intel_pstate/max_perf_pct
echo 100 > "$host"/sys/devices/system/cpu/intel_pstate/min_perf_pct
[ -w "$host"/sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost ] && \
    echo 0 > "$host"/sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost
for g in "$host"/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
    echo performance > "$g"
done
sleep 0.4
'"$REMOTE_CHECK_SH"

REMOTE_UNPIN_SH='
host=/host
echo 0 > "$host"/sys/devices/system/cpu/intel_pstate/no_turbo
for g in "$host"/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
    echo powersave > "$g"
done
sleep 0.4
'"$REMOTE_CHECK_SH"

# ------------- multi-node via kubectl -------------

multi_node_run() {
    local sub="$1"
    local body=""
    case "$sub" in
        check)   body="$REMOTE_CHECK_SH" ;;
        pin)     body="$REMOTE_PIN_SH" ;;
        unpin)   body="$REMOTE_UNPIN_SH" ;;
        persist)
            echo "ERROR: 'persist --all' not supported (needs systemd nsenter)." >&2
            echo "       Bootstrap SSH then run 'pin-cpu-22ghz.sh persist' per node, or" >&2
            echo "       just re-run 'pin-cpu-22ghz.sh pin --all' after reboots." >&2
            exit 1 ;;
        *) echo "internal: unknown sub: $sub" >&2; exit 1 ;;
    esac

    local nodes
    nodes=$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
    if [[ -z "$nodes" ]]; then
        echo "ERROR: kubectl returned no nodes" >&2
        exit 3
    fi

    local b64; b64=$(printf '%s' "$body" | base64 -w0)

    # Pre-clean any stale pods from a prior crashed run.
    kubectl delete pod -l "app=$POD_LABEL" --grace-period=0 --wait=false >/dev/null 2>&1 || true

    local pods=()
    for node in $nodes; do
        local podname="${POD_LABEL}-${node}"
        pods+=("$podname")
        kubectl apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $podname
  labels:
    app: $POD_LABEL
spec:
  nodeName: $node
  restartPolicy: Never
  hostPID: true
  tolerations:
    - operator: Exists
  containers:
    - name: runner
      image: $POD_IMAGE
      securityContext:
        privileged: true
      command: ["/bin/sh","-c"]
      args:
        - "echo $b64 | base64 -d | sh"
      volumeMounts:
        - { name: sysfs,  mountPath: /host/sys }
        - { name: procfs, mountPath: /host/proc }
  volumes:
    - { name: sysfs,  hostPath: { path: /sys } }
    - { name: procfs, hostPath: { path: /proc } }
EOF
    done

    local rc=0
    for podname in "${pods[@]}"; do
        local node=${podname#${POD_LABEL}-}
        printf '=== %s ===\n' "$node"
        if ! kubectl wait "pod/$podname" --for=jsonpath='{.status.phase}'=Succeeded --timeout=90s >/dev/null 2>&1; then
            local phase; phase=$(kubectl get "pod/$podname" -o jsonpath='{.status.phase}' 2>/dev/null || echo Unknown)
            echo "[pod ended phase=$phase]" >&2
            rc=1
        fi
        kubectl logs "$podname" 2>&1 || true
        kubectl delete "pod/$podname" --grace-period=0 --wait=false >/dev/null 2>&1 || true
    done
    return "$rc"
}

# ------------- arg parsing -------------

sub=""
do_all=0
for a in "$@"; do
    case "$a" in
        pin|check|persist|unpin) sub="$a" ;;
        --all)                   do_all=1 ;;
        -h|--help)               usage 0 ;;
        *) echo "unknown arg: $a" >&2; usage 1 ;;
    esac
done
[[ -z "$sub" ]] && usage 1

# ------------- dispatch -------------

if [[ "$do_all" -eq 1 ]]; then
    multi_node_run "$sub"
    exit $?
fi

# Local; elevate to root for everything except `check`.
if [[ "$sub" != "check" && "$EUID" -ne 0 ]]; then
    exec sudo -E "$THIS" "$sub"
fi

"cmd_$sub"
