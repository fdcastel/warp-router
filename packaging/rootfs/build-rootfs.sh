#!/usr/bin/env bash
# Build a Debian 13 (Trixie) rootfs with all Warp Router packages.
#
# Usage: sudo ./build-rootfs.sh <output-dir>
#
# Requires: mmdebstrap, running as root (or in a user namespace).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGES_LIST="${SCRIPT_DIR}/packages.list"
HOOKS_DIR="${SCRIPT_DIR}/hooks"
OVERLAY_DIR="${SCRIPT_DIR}/overlay"

SUITE="trixie"
MIRROR="http://deb.debian.org/debian"
COMPONENTS="main,contrib,non-free-firmware"

# --- Argument parsing ---

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <output-dir>" >&2
    exit 1
fi

OUTPUT_DIR="$1"

if [[ $EUID -ne 0 ]]; then
    echo "Error: This script must run as root (or via sudo)." >&2
    exit 1
fi

# --- Read package list ---

PACKAGES=""
while IFS= read -r line; do
    # Strip comments and whitespace
    line="${line%%#*}"
    line="${line// /}"
    [[ -z "$line" ]] && continue
    if [[ -z "$PACKAGES" ]]; then
        PACKAGES="$line"
    else
        PACKAGES="${PACKAGES},$line"
    fi
done < "$PACKAGES_LIST"

echo "==> Building Debian ${SUITE} rootfs into ${OUTPUT_DIR}"
echo "==> Packages: ${PACKAGES}"

# --- Build rootfs with mmdebstrap ---

HOOK_ARGS=()
if [[ -d "$HOOKS_DIR" ]]; then
    for hook in "$HOOKS_DIR"/customize*.sh; do
        [[ -f "$hook" ]] && HOOK_ARGS+=(--customize-hook="$hook \"\$1\"")
    done
fi

KEYRING_ARGS=()
if [[ -f "/usr/share/keyrings/debian-archive-keyring.gpg" ]]; then
    KEYRING_ARGS+=(--keyring=/usr/share/keyrings/debian-archive-keyring.gpg)
fi

mmdebstrap \
    --variant=minbase \
    --components="${COMPONENTS}" \
    --include="${PACKAGES}" \
    "${KEYRING_ARGS[@]}" \
    "${HOOK_ARGS[@]}" \
    "$SUITE" \
    "$OUTPUT_DIR" \
    "$MIRROR"

# --- Apply overlay ---

if [[ -d "$OVERLAY_DIR" ]]; then
    echo "==> Applying overlay from ${OVERLAY_DIR}"
    cp -a "${OVERLAY_DIR}/." "${OUTPUT_DIR}/"
fi

# --- Run post-overlay hooks ---

if [[ -d "$HOOKS_DIR" ]]; then
    for hook in "$HOOKS_DIR"/post-overlay*.sh; do
        if [[ -f "$hook" ]]; then
            echo "==> Running post-overlay hook: $(basename "$hook")"
            "$hook" "$OUTPUT_DIR"
        fi
    done
fi

# --- Install warp binary (if present in build/) ---

WARP_BIN="${SCRIPT_DIR}/../../build/warp"
if [[ -f "$WARP_BIN" ]]; then
    echo "==> Installing warp binary"
    install -d -m 0755 "${OUTPUT_DIR}/usr/bin" "${OUTPUT_DIR}/usr/local/bin"
    # Install in /usr/bin so non-login shells (e.g. pct exec/ssh command mode)
    # can resolve `warp` with the default PATH.
    install -m 0755 "$WARP_BIN" "${OUTPUT_DIR}/usr/bin/warp"
    ln -sf /usr/bin/warp "${OUTPUT_DIR}/usr/local/bin/warp"
else
    echo "==> WARNING: warp binary not found at ${WARP_BIN}, skipping installation"
fi

echo "==> Rootfs build complete: ${OUTPUT_DIR}"
