#!/usr/bin/env bash
# Build a Proxmox-compatible LXC template from a rootfs directory.
#
# Usage: ./build-lxc.sh <rootfs-dir> <output-tar-zst>
#
# Produces a .tar.zst archive suitable for use with `pct create`.

set -euo pipefail

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <rootfs-dir> <output-tar-zst>" >&2
    exit 1
fi

ROOTFS_DIR="$1"
OUTPUT_FILE="$2"

if [[ ! -d "$ROOTFS_DIR" ]]; then
    echo "Error: rootfs directory does not exist: ${ROOTFS_DIR}" >&2
    exit 1
fi

# --- Ensure required rootfs files exist ---

# Proxmox LXC expects /etc/hostname
if [[ ! -f "${ROOTFS_DIR}/etc/hostname" ]]; then
    echo "warp-router" > "${ROOTFS_DIR}/etc/hostname"
fi

# Proxmox LXC expects /etc/network/interfaces (even if unused with systemd-networkd)
mkdir -p "${ROOTFS_DIR}/etc/network"
if [[ ! -f "${ROOTFS_DIR}/etc/network/interfaces" ]]; then
    cat > "${ROOTFS_DIR}/etc/network/interfaces" <<'EOF'
# Managed by Proxmox / systemd-networkd.
# This file is intentionally minimal.
auto lo
iface lo inet loopback
EOF
fi

# Ensure resolv.conf exists (Proxmox will overwrite it)
if [[ ! -f "${ROOTFS_DIR}/etc/resolv.conf" ]]; then
    echo "# Populated by Proxmox" > "${ROOTFS_DIR}/etc/resolv.conf"
fi

# --- Create output directory ---

OUTPUT_DIR="$(dirname "$OUTPUT_FILE")"
mkdir -p "$OUTPUT_DIR"

# --- Build the archive ---

echo "==> Creating LXC template: ${OUTPUT_FILE}"
tar --create \
    --zstd \
    --numeric-owner \
    --xattrs \
    --directory="$ROOTFS_DIR" \
    --file="$OUTPUT_FILE" \
    .

echo "==> LXC template created: ${OUTPUT_FILE} ($(du -h "$OUTPUT_FILE" | cut -f1))"
