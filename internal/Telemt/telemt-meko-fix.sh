#!/usr/bin/env bash
set -euo pipefail

CONFIG_GLOB="/usr/local/x-ui/bin/mtproto/telemt-*.toml"
LEGACY_CONFIG="/etc/x-ui/telemt.toml"
STATE_FILE="/etc/x-ui/telemt-meko-fix.env"
FILTER_CHAIN="TELEMT_MEKO"
MARK_CHAIN="TELEMT_MEKO_MARK"
MARK="0x400"
U32_FILTER="32 & 0x000FFFFF = 0x0002FFFF && 40 & 0xFF000000 = 0x02000000 && 44 & 0xFFFF0000 = 0x01030000 && 48 & 0xFFFFFF00 = 0x01010800 && 60 & 0xFFFFFFFF = 0x04020000"

# Load panel-managed settings for direct/manual invocations too. Explicit
# environment values still win, which lets the backend preview/apply settings
# atomically before the systemd unit is restarted.
file_enabled=""
file_rate=""
file_burst=""
if [[ -r "$STATE_FILE" ]]; then
    while IFS='=' read -r key value; do
        case "${key//[[:space:]]/}" in
            TELEMT_MEKO_ENABLED) file_enabled="${value//[[:space:]]/}" ;;
            TELEMT_MEKO_RATE) file_rate="${value//[[:space:]]/}" ;;
            TELEMT_MEKO_BURST) file_burst="${value//[[:space:]]/}" ;;
        esac
    done < "$STATE_FILE"
fi
ENABLED="${TELEMT_MEKO_ENABLED:-${file_enabled:-0}}"
RATE="${TELEMT_MEKO_RATE:-${file_rate:-54/minute}}"
BURST="${TELEMT_MEKO_BURST:-${file_burst:-1}}"

log() { printf '%s\n' "[telemt-meko-fix] $*"; }

enabled() {
    case "${ENABLED,,}" in
        1|true|yes|on) return 0 ;;
        *) return 1 ;;
    esac
}

normalize_rate() {
    local value="$1" number
    case "$value" in
        */minute) number="${value%/minute}" ;;
        */min) number="${value%/min}" ;;
        *) number="$value" ;;
    esac
    [[ "$number" =~ ^[0-9]+$ ]] || return 1
    (( 10#$number >= 1 && 10#$number <= 60000 )) || return 1
    printf '%d/minute\n' "$((10#$number))"
}

normalize_burst() {
    local value="$1"
    [[ "$value" =~ ^[0-9]+$ ]] || return 1
    (( 10#$value >= 1 && 10#$value <= 1000 )) || return 1
    printf '%d\n' "$((10#$value))"
}

normalize_limits() {
    local raw_rate="$RATE" raw_burst="$BURST"
    if ! RATE="$(normalize_rate "$raw_rate")"; then
        log "Invalid TELEMT_MEKO_RATE: $raw_rate"
        return 2
    fi
    if ! BURST="$(normalize_burst "$raw_burst")"; then
        log "Invalid TELEMT_MEKO_BURST: $raw_burst"
        return 2
    fi
}

read_port() {
    local config="$1"
    awk '
        /^\[server\][[:space:]]*$/ { in_server=1; next }
        /^\[/ { in_server=0 }
        in_server && /^[[:space:]]*port[[:space:]]*=/ {
            line=$0
            sub(/^[^=]*=/, "", line)
            gsub(/[[:space:]]/, "", line)
            print line
            exit
        }
    ' "$config"
}

valid_port() {
    [[ "$1" =~ ^[0-9]+$ ]] && (( 10#$1 >= 1 && 10#$1 <= 65535 ))
}

collect_ports() {
    local -a found=()
    local p f

    if [[ -n "${TELEMT_PORTS:-}" ]]; then
        IFS=', ' read -r -a found <<< "$TELEMT_PORTS"
    else
        shopt -s nullglob
        for f in $CONFIG_GLOB; do
            p="$(read_port "$f" || true)"
            valid_port "$p" && found+=("$p")
        done
        shopt -u nullglob

        if [[ -r "$LEGACY_CONFIG" ]] && command -v systemctl >/dev/null 2>&1 && \
           systemctl is-active --quiet telemt.service 2>/dev/null; then
            p="$(read_port "$LEGACY_CONFIG" || true)"
            valid_port "$p" && found+=("$p")
        fi
    fi

    printf '%s\n' "${found[@]}" | awk '/^[0-9]+$/ && $1>=1 && $1<=65535 { seen[$1]=1 } END { for (p in seen) print p }' | sort -n
}

ensure_u32() {
    if ! iptables -m u32 --help >/dev/null 2>&1; then
        modprobe xt_u32 >/dev/null 2>&1 || true
    fi
    iptables -m u32 --help >/dev/null 2>&1
}

remove_jump_all() {
    local table="$1" parent="$2" child="$3"
    while iptables -t "$table" -C "$parent" -j "$child" 2>/dev/null; do
        iptables -t "$table" -D "$parent" -j "$child" 2>/dev/null || break
    done
}

# A firewall manager may insert rules ahead of MEKO after it was applied.
# Merely checking that the jump exists is therefore insufficient: an earlier
# ACCEPT can bypass the filter. Healthy means exactly one jump, at rule #1.
jump_is_first_unique() {
    local table="$1" parent="$2" child="$3"
    iptables -t "$table" -L "$parent" --line-numbers -n 2>/dev/null | awk -v child="$child" '
        $1 ~ /^[0-9]+$/ && $2 == child {
            count++
            if ($1 == 1) first = 1
        }
        END { exit !(count == 1 && first == 1) }
    '
}

ensure_chain() {
    local table="$1" chain="$2"
    iptables -t "$table" -N "$chain" 2>/dev/null || true
    iptables -t "$table" -F "$chain"
}

remove() {
    command -v iptables >/dev/null 2>&1 || return 0
    remove_jump_all filter INPUT "$FILTER_CHAIN"
    remove_jump_all mangle PREROUTING "$MARK_CHAIN"
    iptables -t filter -F "$FILTER_CHAIN" 2>/dev/null || true
    iptables -t filter -X "$FILTER_CHAIN" 2>/dev/null || true
    iptables -t mangle -F "$MARK_CHAIN" 2>/dev/null || true
    iptables -t mangle -X "$MARK_CHAIN" 2>/dev/null || true
    log "MEKO V3 rules removed"
}

apply() {
    if ! enabled; then
        remove
        log "MEKO V3 is disabled by panel settings"
        return 0
    fi

    # Rate/burst only matter while adding hashlimit rules. Keep cleanup usable
    # even if the persisted state was edited or corrupted by hand.
    normalize_limits

    command -v iptables >/dev/null 2>&1 || { log "iptables is required"; exit 1; }
    ensure_u32 || { log "xt_u32 is not available; MEKO V3 cannot be enabled"; exit 1; }

    mapfile -t ports < <(collect_ports)
    if (( ${#ports[@]} == 0 )); then
        remove
        log "No active Telemt MTProto ports found; rules left clean"
        return 0
    fi

    ensure_chain filter "$FILTER_CHAIN"
    ensure_chain mangle "$MARK_CHAIN"

    # Keep exactly one jump and make it the first rule. This is intentionally
    # idempotent and also repairs ordering changed by ufw/firewalld/scripts.
    remove_jump_all filter INPUT "$FILTER_CHAIN"
    remove_jump_all mangle PREROUTING "$MARK_CHAIN"
    iptables -t mangle -I PREROUTING 1 -j "$MARK_CHAIN"
    # Upstream MEKO used INPUT position 2 only because it first inserted its
    # own global SSH ACCEPT rule at position 1. 3x-ui intentionally does not
    # mutate SSH policy, so the MEKO chain itself must run before any existing
    # INPUT ACCEPT rule or Telemt SYN packets could bypass the fix entirely.
    iptables -t filter -I INPUT 1 -j "$FILTER_CHAIN"

    local port
    for port in "${ports[@]}"; do
        valid_port "$port" || continue

        # Preserve every unrelated fwmark bit. MEKO owns only bit 0x400.
        iptables -t mangle -A "$MARK_CHAIN" -p tcp --dport "$port" -m u32 --u32 "$U32_FILTER" \
            -j MARK --set-xmark "$MARK/$MARK"
        iptables -t filter -A "$FILTER_CHAIN" -p tcp --dport "$port" --syn -m mark --mark "$MARK/$MARK" -j ACCEPT

        # xt_hashlimit stores names in an IFNAMSIZ-sized field (15 visible
        # characters on Linux). Keep the per-port name short even for 65535.
        iptables -t filter -A "$FILTER_CHAIN" -p tcp --dport "$port" --syn \
            -m hashlimit --hashlimit-name "tm_${port}" --hashlimit-mode srcip \
            --hashlimit-upto "$RATE" --hashlimit-burst "$BURST" \
            --hashlimit-htable-expire 60000 --hashlimit-htable-size 32768 -j ACCEPT
        iptables -t filter -A "$FILTER_CHAIN" -p tcp --dport "$port" --syn \
            -j REJECT --reject-with tcp-reset
    done

    log "MEKO V3 applied to Telemt MTProto ports: ${ports[*]} (rate=${RATE}, burst=${BURST})"
}

status() {
    local installed=false
    local -a applied_ports=()
    if command -v iptables >/dev/null 2>&1; then
        if jump_is_first_unique filter INPUT "$FILTER_CHAIN" && \
           jump_is_first_unique mangle PREROUTING "$MARK_CHAIN"; then
            installed=true
        fi
        if iptables -t filter -S "$FILTER_CHAIN" >/dev/null 2>&1; then
            mapfile -t applied_ports < <(
                iptables -t filter -S "$FILTER_CHAIN" 2>/dev/null \
                    | awk '{ for (i=1; i<=NF; i++) if ($i=="--dport") print $(i+1) }' \
                    | awk '/^[0-9]+$/ { seen[$1]=1 } END { for (p in seen) print p }' \
                    | sort -n
            )
        fi
    fi
    mapfile -t ports < <(collect_ports)
    printf 'enabled=%s\n' "$(enabled && echo true || echo false)"
    printf 'installed=%s\n' "$installed"
    printf 'ports=%s\n' "$(IFS=,; echo "${ports[*]:-}")"
    printf 'applied_ports=%s\n' "$(IFS=,; echo "${applied_ports[*]:-}")"
    printf 'rate=%s\n' "$RATE"
    printf 'burst=%s\n' "$BURST"
}

case "${1:-apply}" in
    apply) apply ;;
    remove) remove ;;
    status) status ;;
    *) echo "Usage: $0 {apply|remove|status}" >&2; exit 2 ;;
esac
