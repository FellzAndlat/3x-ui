#!/usr/bin/env bash
# Read-only snapshot of the panel service and its child processes. Run on the
# host as root: bash tools/memory-report.sh. No configuration or secrets printed.
set -euo pipefail

service=${1:-x-ui}
if [[ ! $service =~ ^[a-zA-Z0-9_.@-]+$ ]]; then
    printf 'Invalid service name\n' >&2
    exit 2
fi

main_pid=$(systemctl show "$service" --value -p MainPID)
if [[ ! $main_pid =~ ^[0-9]+$ ]] || (( main_pid == 0 )); then
    printf 'Service %s is not running\n' "$service" >&2
    exit 1
fi

printf 'Service: %s\n' "$service"
printf 'MemoryCurrent: %s bytes\n' "$(systemctl show "$service" --value -p MemoryCurrent)"
printf 'MemoryPeak: %s bytes\n' "$(systemctl show "$service" --value -p MemoryPeak)"
printf 'MemoryHigh: %s bytes\n' "$(systemctl show "$service" --value -p MemoryHigh)"
printf 'MainPID: %s\n' "$main_pid"
printf '%-9s %-17s %12s %12s %12s %12s\n' PID COMM RSS_KiB PSS_KiB PRIVATE_KiB SWAP_KiB

# Enumerate descendants through /proc so unrelated processes with the same
# executable name are excluded. A process may exit during a snapshot.
pending=("$main_pid")
for ((i = 0; i < ${#pending[@]}; i++)); do
    pid=${pending[i]}
    [[ -r /proc/$pid/status ]] || continue
    for status in /proc/[0-9]*/status; do
        [[ -r $status ]] || continue
        child=${status#/proc/}
        child=${child%/status}
        parent=$(awk '/^PPid:/ {print $2; exit}' "$status" 2>/dev/null) || continue
        if [[ $parent == "$pid" ]]; then
            pending+=("$child")
        fi
    done

    comm=$(cat "/proc/$pid/comm" 2>/dev/null) || continue
    rss=$(awk '/^VmRSS:/ {print $2; exit}' "/proc/$pid/status" 2>/dev/null)
    rss=${rss:-0}
    pss=0 private=0 swap=0
    if [[ -r /proc/$pid/smaps_rollup ]]; then
        read -r pss private swap < <(awk '
            /^Pss:/ {pss=$2}
            /^Private_Clean:|^Private_Dirty:/ {private+=$2}
            /^Swap:/ {swap=$2}
            END {print pss+0, private+0, swap+0}
        ' "/proc/$pid/smaps_rollup")
    fi
    printf '%-9s %-17s %12s %12s %12s %12s\n' "$pid" "$comm" "$rss" "$pss" "$private" "$swap"
done
