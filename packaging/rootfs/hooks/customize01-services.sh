#!/usr/bin/env bash
# mmdebstrap customize hook — runs inside the rootfs chroot.
# Usage: This script is called by mmdebstrap with $1 = rootfs directory.

set -euo pipefail

ROOTFS="$1"

echo "==> Running customize hook on ${ROOTFS}"

# --- Enable IP forwarding sysctl ---
# (Already placed by overlay, but ensure the directory exists)
mkdir -p "${ROOTFS}/etc/sysctl.d"

# --- Enable required services ---
chroot "${ROOTFS}" systemctl enable frr
chroot "${ROOTFS}" systemctl enable nftables
chroot "${ROOTFS}" systemctl enable kea-dhcp4-server
chroot "${ROOTFS}" systemctl enable unbound
chroot "${ROOTFS}" systemctl enable ssh
chroot "${ROOTFS}" systemctl enable cloud-init-local.service 2>/dev/null || true
chroot "${ROOTFS}" systemctl enable cloud-init.service 2>/dev/null || true
chroot "${ROOTFS}" systemctl enable cloud-config.service 2>/dev/null || true
chroot "${ROOTFS}" systemctl enable cloud-final.service 2>/dev/null || true
chroot "${ROOTFS}" systemctl enable systemd-networkd

# --- Set timezone to UTC ---
chroot "${ROOTFS}" ln -sf /usr/share/zoneinfo/UTC /etc/localtime
echo "UTC" > "${ROOTFS}/etc/timezone"

# --- Set locale ---
echo "en_US.UTF-8 UTF-8" > "${ROOTFS}/etc/locale.gen"
chroot "${ROOTFS}" locale-gen 2>/dev/null || true

# --- Lock root password (key-only SSH) ---
chroot "${ROOTFS}" passwd -l root

# --- Create warp config directory ---
mkdir -p "${ROOTFS}/etc/warp"
mkdir -p "${ROOTFS}/var/lib/warp/revisions"

# --- Clean APT cache ---
chroot "${ROOTFS}" apt-get clean
rm -rf "${ROOTFS}/var/lib/apt/lists/"*

# --- Remove machine-id (will be regenerated on first boot) ---
truncate -s 0 "${ROOTFS}/etc/machine-id"
rm -f "${ROOTFS}/var/lib/dbus/machine-id"

# --- Remove SSH host keys (will be regenerated on first boot) ---
rm -f "${ROOTFS}"/etc/ssh/ssh_host_*

echo "==> Customize hook complete"
