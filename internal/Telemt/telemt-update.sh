#!/bin/bash
set -euo pipefail

BIN="/usr/local/x-ui/bin/telemt"
VERSION_FILE="/etc/x-ui/telemt.version"
TMP_DIR="/tmp/telemt-update.$$"
REPO="telemt/telemt"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT
log() { echo "[telemt-update] $*"; }

arch_name() {
  case "$(uname -m)" in
    x86_64|amd64) echo "x86_64" ;;
    aarch64|arm64) echo "aarch64" ;;
    *) echo "unsupported" ;;
  esac
}

get_latest_tag() {
  curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 -o /dev/null -w "%{url_effective}" "https://github.com/${REPO}/releases/latest" | sed -n "s#.*/releases/tag/##p"
}

download_latest() {
  local arch="$1" tag="$2"
  local asset="telemt-${arch}-linux-musl.tar.gz"
  local base="https://github.com/${REPO}/releases/download/${tag}"
  mkdir -p "$TMP_DIR"
  curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 -o "$TMP_DIR/$asset" "$base/$asset"
  curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 -o "$TMP_DIR/$asset.sha256" "$base/$asset.sha256"
  ( cd "$TMP_DIR"; sha256sum -c "$asset.sha256" )
  tar -xzf "$TMP_DIR/$asset" -C "$TMP_DIR"
  test -s "$TMP_DIR/telemt"
  chmod 0755 "$TMP_DIR/telemt"
}

current_version() {
  if [[ -s "$VERSION_FILE" ]]; then tr -d "[:space:]" < "$VERSION_FILE"; return; fi
  if [[ -x "$BIN" ]]; then "$BIN" --version 2>/dev/null | sed -n "s/.*\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p" | head -n1; fi
}

install_latest() {
  local tag="$1"
  install -d -m 0755 -o root -g root "$(dirname "$BIN")"
  install -m 0755 -o root -g root "$TMP_DIR/telemt" "${BIN}.new"
  mv -f "${BIN}.new" "$BIN"
  printf "%s\n" "${tag#v}" > "$VERSION_FILE"
  chown root:root "$VERSION_FILE"
  chmod 0644 "$VERSION_FILE"
}

recycle_inbound_sidecars() {
  # MTProto inbounds are child Telemt processes supervised by x-ui rather than
  # systemd units. Terminating only processes whose config lives in the panel's
  # mtproto directory makes the manager respawn them with the freshly installed
  # binary on its next reconcile cycle, without restarting the panel itself.
  local pattern="${BIN} run /usr/local/x-ui/bin/mtproto/telemt-"
  local pids=""
  if command -v pgrep >/dev/null 2>&1; then
    pids="$(pgrep -f "$pattern" 2>/dev/null || true)"
  else
    pids="$(ps -eo pid=,args= 2>/dev/null | awk -v p="$pattern" 'index($0,p) {print $1}')"
  fi
  [[ -n "$pids" ]] || return 0
  log "recycling MTProto Telemt sidecars: $(echo "$pids" | tr '\n' ' ')"
  while read -r pid; do
    [[ "$pid" =~ ^[0-9]+$ ]] || continue
    kill -TERM "$pid" 2>/dev/null || true
  done <<< "$pids"
}

main() {
  [[ "$EUID" -eq 0 ]] || { echo "must run as root" >&2; exit 1; }
  local arch tag current was_active=0
  arch="$(arch_name)"
  [[ "$arch" != "unsupported" ]] || { echo "unsupported CPU architecture" >&2; exit 1; }
  tag="$(get_latest_tag)"
  [[ "$tag" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid latest Telemt tag: $tag" >&2; exit 1; }
  current="$(current_version || true)"
  if [[ "${1:-}" == "--check" ]]; then
    echo "current=${current:-unknown}"
    echo "latest=${tag#v}"
    [[ "${current#v}" == "${tag#v}" ]]
    exit $?
  fi
  if [[ "${current#v}" == "${tag#v}" && -x "$BIN" ]]; then log "Telemt ${current#v} is already current."; exit 0; fi
  log "updating Telemt ${current:-unknown} -> ${tag#v}"
  download_latest "$arch" "$tag"
  if systemctl is-active --quiet telemt.service 2>/dev/null; then
    was_active=1
    systemctl stop telemt.service
  fi
  install_latest "$tag"
  systemctl daemon-reload
  if (( was_active )); then systemctl start telemt.service; fi
  recycle_inbound_sidecars
  log "Telemt ${tag#v} installed."
}
main "$@"
