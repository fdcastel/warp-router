#!/usr/bin/env bash
# Build a QCOW2 VM image from a rootfs directory.
#
# Usage: ./build-qcow2.sh <rootfs-dir> <output-qcow2>
#
# Requires: qemu-img, virt-make-fs (from libguestfs-tools), grub-install or
#            virt-customize with GRUB injection, and cloud-init seed.
#
# Produces a QCOW2 image with:
#   - GPT partition table
#   - EFI System Partition (ESP) + root ext4 partition
#   - GRUB bootloader (EFI + BIOS fallback)
#   - cloud-init NoCloud datasource pre-configured

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLOUD_INIT_SEED="${SCRIPT_DIR}/../cloud-init/seed"

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <rootfs-dir> <output-qcow2>" >&2
    exit 1
fi

ROOTFS_DIR="$1"
OUTPUT_FILE="$2"

if [[ ! -d "$ROOTFS_DIR" ]]; then
    echo "Error: rootfs directory does not exist: ${ROOTFS_DIR}" >&2
    exit 1
fi

# --- Create output directory ---

OUTPUT_DIR="$(dirname "$OUTPUT_FILE")"
mkdir -p "$OUTPUT_DIR"

DISK_SIZE="4G"
RAW_IMAGE="${OUTPUT_FILE%.qcow2}.raw"

echo "==> Building QCOW2 image from ${ROOTFS_DIR}"

# --- Prepare rootfs for VM boot ---

# Ensure fstab exists
if [[ ! -f "${ROOTFS_DIR}/etc/fstab" ]]; then
    cat > "${ROOTFS_DIR}/etc/fstab" <<'EOF'
# <file system>  <mount point>  <type>  <options>         <dump>  <pass>
UUID=ROOT_UUID   /              ext4    errors=remount-ro 0       1
EOF
fi

# Ensure cloud-init datasource is configured for NoCloud
mkdir -p "${ROOTFS_DIR}/etc/cloud/cloud.cfg.d"
cat > "${ROOTFS_DIR}/etc/cloud/cloud.cfg.d/99-warp-router.cfg" <<'EOF'
# Warp Router cloud-init datasource configuration
datasource_list: [ NoCloud, ConfigDrive, None ]
datasource:
  NoCloud:
    seedfrom: /var/lib/cloud/seed/nocloud/
EOF

# Copy cloud-init seed templates if available
if [[ -d "$CLOUD_INIT_SEED" ]]; then
    mkdir -p "${ROOTFS_DIR}/var/lib/cloud/seed/nocloud"
    cp -a "${CLOUD_INIT_SEED}/." "${ROOTFS_DIR}/var/lib/cloud/seed/nocloud/"
fi

# --- Create raw disk image ---

echo "==> Creating raw disk image (${DISK_SIZE})"
truncate -s "$DISK_SIZE" "$RAW_IMAGE"

# Create GPT partition table with BIOS boot + EFI + root partitions
sgdisk --clear \
    --new=1::+1M   --typecode=1:ef02 --change-name=1:"BIOS boot" \
    --new=2::+256M  --typecode=2:ef00 --change-name=2:"EFI System" \
    --new=3::0      --typecode=3:8300 --change-name=3:"Linux root" \
    "$RAW_IMAGE"

# --- Set up loop device ---

LOOP_DEV=$(losetup --find --show --partscan "$RAW_IMAGE")
echo "==> Loop device: ${LOOP_DEV}"

cleanup() {
    set +e
    umount "${MOUNT_DIR}/boot/efi" 2>/dev/null
    umount "${MOUNT_DIR}/dev" 2>/dev/null
    umount "${MOUNT_DIR}/proc" 2>/dev/null
    umount "${MOUNT_DIR}/sys" 2>/dev/null
    umount "$MOUNT_DIR" 2>/dev/null
    losetup -d "$LOOP_DEV" 2>/dev/null
    rm -rf "$MOUNT_DIR"
    rm -f "$RAW_IMAGE"
}
trap cleanup EXIT

# --- Format partitions ---

mkfs.vfat -F 32 "${LOOP_DEV}p2"
mkfs.ext4 -q -L warp-root "${LOOP_DEV}p3"

ROOT_UUID=$(blkid -s UUID -o value "${LOOP_DEV}p3")

# --- Mount and copy rootfs ---

MOUNT_DIR=$(mktemp -d)
mount "${LOOP_DEV}p3" "$MOUNT_DIR"
mkdir -p "${MOUNT_DIR}/boot/efi"
mount "${LOOP_DEV}p2" "${MOUNT_DIR}/boot/efi"

echo "==> Copying rootfs"
cp -a "${ROOTFS_DIR}/." "${MOUNT_DIR}/"

# Update fstab with real UUID
sed -i "s/ROOT_UUID/${ROOT_UUID}/" "${MOUNT_DIR}/etc/fstab"

# --- Install GRUB ---

echo "==> Installing GRUB bootloader"
mount --bind /dev "${MOUNT_DIR}/dev"
mount --bind /proc "${MOUNT_DIR}/proc"
mount --bind /sys "${MOUNT_DIR}/sys"

chroot "$MOUNT_DIR" grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=warp --no-floppy --removable 2>/dev/null || true
chroot "$MOUNT_DIR" grub-install --target=i386-pc "$LOOP_DEV" 2>/dev/null || true
chroot "$MOUNT_DIR" update-grub 2>/dev/null || true

# --- Unmount ---

umount "${MOUNT_DIR}/sys"
umount "${MOUNT_DIR}/proc"
umount "${MOUNT_DIR}/dev"
umount "${MOUNT_DIR}/boot/efi"
umount "$MOUNT_DIR"

# --- Convert to QCOW2 ---

echo "==> Converting to QCOW2"
qemu-img convert -f raw -O qcow2 -c "$RAW_IMAGE" "$OUTPUT_FILE"

echo "==> QCOW2 image created: ${OUTPUT_FILE} ($(du -h "$OUTPUT_FILE" | cut -f1))"
