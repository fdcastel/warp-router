#!/usr/bin/env bash
set -euo pipefail

REPO="fdcastel/warp-router"
CT_ID=9100
CT_NAME="warp-router"
WAN_BRIDGE="vmbr0"
LAN_BRIDGE="vmbrloc0"
LAN_CIDR="192.168.50.1/24"
ROOTFS_STORAGE="spool-zfs"
CLIENT_ID=9110
FORCE=0
IMAGE_URL=""

usage() {
  cat <<'EOF'
Usage:
  test/test-release.sh [--force] [IMAGE_URL]

Description:
  Reproduces the README LXC provisioning flow for CT 9100 and validates router operation.

Arguments:
  IMAGE_URL   Optional direct URL to a .tar.zst release asset.
              If omitted, the script uses the latest GitHub release LXC asset URL.

Options:
  --force     Stop and destroy existing CT 9100 before creating a new one.
  -h, --help  Show this help.
EOF
}

log() {
  printf '\n[%s] %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" "$*" >&2
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --force)
        FORCE=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      http://*|https://*)
        [[ -z "$IMAGE_URL" ]] || die "IMAGE_URL provided more than once"
        IMAGE_URL="$1"
        shift
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done
}

get_latest_image_url() {
  local api_url
  api_url="https://api.github.com/repos/${REPO}/releases/latest"

  if command -v jq >/dev/null 2>&1; then
    curl -fsSL "$api_url" \
      | jq -r '.assets[] | select(.name | test("lxc-amd64\\.tar\\.zst$")) | .browser_download_url' \
      | head -n1
    return
  fi

  curl -fsSL "$api_url" \
    | grep -Eo 'https://[^" ]+lxc-amd64\.tar\.zst' \
    | head -n1
}

require_host_prereqs() {
  [[ "$(id -u)" -eq 0 ]] || die "Run as root on a Proxmox host"

  need_cmd pct
  need_cmd curl
  need_cmd sed
  need_cmd grep
  need_cmd ip

  [[ -d /var/lib/vz/template/cache ]] || die "Expected template cache dir /var/lib/vz/template/cache"
}

ensure_vmbrloc0() {
  ip link show "$LAN_BRIDGE" >/dev/null 2>&1 || die "Bridge $LAN_BRIDGE not found"
}

handle_existing_ct() {
  if pct status "$CT_ID" >/dev/null 2>&1; then
    if [[ "$FORCE" -ne 1 ]]; then
      die "CT $CT_ID already exists. Re-run with --force to stop/destroy it."
    fi

    log "Stopping and destroying existing CT $CT_ID (--force)"
    pct stop "$CT_ID" --skiplock 1 >/dev/null 2>&1 || true
    pct destroy "$CT_ID" --force 1 --purge 1 >/dev/null 2>&1 || true
  fi
}

download_template() {
  local template_file
  template_file="$(basename "$IMAGE_URL")"
  [[ "$template_file" == *.tar.zst ]] || die "IMAGE_URL must point to a .tar.zst file"

  log "Downloading template: $template_file"
  curl -fL "$IMAGE_URL" -o "/var/lib/vz/template/cache/$template_file"

  echo "$template_file"
}

create_router_ct() {
  local template_file="$1"

  log "Loading Proxmox automation helper"
  source <(curl -Ls https://bit.ly/p-v-a)

  log "Creating CT $CT_ID from $template_file"
  ./new-ct.sh "$CT_ID" \
    --ostemplate "local:vztmpl/$template_file" \
    --hostname "$CT_NAME" \
    --sshkeys /root/.ssh/id_ed25519.pub \
    --rootfs "$ROOTFS_STORAGE:8" \
    --bridge "$WAN_BRIDGE" \
    --memory 2048 \
    --cores 2 \
    --net1 "name=eth1,bridge=$LAN_BRIDGE,ip=$LAN_CIDR"
}

apply_site_config() {
  log "Writing and applying site config inside CT $CT_ID"

  pct exec "$CT_ID" -- bash <<'OUTER_EOF'
cat > /etc/warp/site.yaml <<'EOF'
hostname: edge-basic
interfaces:
  - name: wan1
    role: wan
    device: eth0
    address: dhcp

  - name: lan1
    role: lan
    device: eth1
    address: 192.168.50.1/24

dhcp:
  enabled: true
  subnets:
    - subnet: 192.168.50.0/24
      interface: lan1
      pool_start: 192.168.50.100
      pool_end: 192.168.50.199
      gateway: 192.168.50.1
      dns_servers:
        - 192.168.50.1

dns:
  enabled: true
  forwarders:
    - 1.1.1.1
    - 8.8.8.8

ecmp:
  enabled: false
EOF

warp validate /etc/warp/site.yaml
warp apply /etc/warp/site.yaml
warp status
OUTER_EOF
}

assert_warp_command_available() {
  log "Asserting warp command resolution in pct exec shell context"

  local out
  out="$(pct exec "$CT_ID" -- bash -c 'echo PATH=$PATH; command -v warp')"
  echo "$out"

  echo "$out" | grep -q 'PATH=/sbin:/bin:/usr/sbin:/usr/bin' || die "Unexpected PATH in pct exec shell"
  echo "$out" | grep -Eq '/(usr/)?bin/warp' || die "warp not resolvable in pct exec shell"
}

assert_services_active() {
  log "Asserting core services are active"

  local svc
  for svc in frr nftables kea-dhcp4-server unbound; do
    pct exec "$CT_ID" -- systemctl is-active "$svc" | grep -q '^active$' || die "Service not active: $svc"
  done
}

assert_lan_dhcp_operational() {
  log "Asserting LAN DHCP and router reachability with temporary client CT"

  if pct status "$CLIENT_ID" >/dev/null 2>&1; then
    pct stop "$CLIENT_ID" --skiplock 1 >/dev/null 2>&1 || true
    pct destroy "$CLIENT_ID" --force 1 --purge 1 >/dev/null 2>&1 || true
  fi

  pct create "$CLIENT_ID" local:vztmpl/debian-12-standard_12.12-1_amd64.tar.zst \
    --hostname lan-client \
    --memory 512 \
    --cores 1 \
    --rootfs "$ROOTFS_STORAGE:4" \
    --net0 "name=eth0,bridge=$LAN_BRIDGE,ip=dhcp" \
    --unprivileged 1 \
    --onboot 0

  pct start "$CLIENT_ID"
  sleep 3

  local out
  out="$(pct exec "$CLIENT_ID" -- bash -c 'ip -4 addr show dev eth0; ip -4 route')"
  echo "$out"

  echo "$out" | grep -q 'inet 192\.168\.50\.' || die "Client did not receive DHCP lease on 192.168.50.0/24"
  echo "$out" | grep -q 'default via 192.168.50.1' || die "Client default route is not router LAN IP"

  pct exec "$CLIENT_ID" -- ping -c 2 -W 2 192.168.50.1 >/dev/null || die "Client cannot reach router LAN IP"

  pct stop "$CLIENT_ID" --skiplock 1 >/dev/null 2>&1 || true
  pct destroy "$CLIENT_ID" --force 1 --purge 1 >/dev/null 2>&1 || true
}

main() {
  parse_args "$@"
  require_host_prereqs
  ensure_vmbrloc0

  if [[ -z "$IMAGE_URL" ]]; then
    log "No IMAGE_URL provided, resolving latest release"
    IMAGE_URL="$(get_latest_image_url)"
    [[ -n "$IMAGE_URL" ]] || die "Could not resolve latest release LXC image URL"
  fi

  log "Using image URL: $IMAGE_URL"
  handle_existing_ct
  local template_file
  template_file="$(download_template)"

  create_router_ct "$template_file"
  apply_site_config
  assert_warp_command_available
  assert_services_active
  assert_lan_dhcp_operational

  log "SUCCESS: release image validated on CT $CT_ID"
}

main "$@"
