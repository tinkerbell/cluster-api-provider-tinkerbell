#!/bin/bash
# Validate a built image by checking for required Kubernetes binaries and configuration.
# Usage: validate.sh --image-name <name> --output-dir <dir> [--arch <amd64|arm64>]
# Backwards-compatible positional form: validate.sh <image-name> <output-dir>
set -euo pipefail

IMAGE_NAME=""
IMAGE_DIR=""
ARCH=""

# Parse flags first; fall back to legacy positional arguments.
if [[ "${1:-}" == --* ]]; then
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --image-name) IMAGE_NAME="$2"; shift 2 ;;
            --output-dir) IMAGE_DIR="$2";  shift 2 ;;
            --arch)       ARCH="$2";       shift 2 ;;
            *) echo "Unknown argument: $1"; exit 1 ;;
        esac
    done
else
    IMAGE_NAME="${1:-k8s-node-ubuntu-2404-amd64}"
    IMAGE_DIR="${2:-mkosi.output}"
fi

echo "=== Validating ${IMAGE_NAME} image${ARCH:+ (arch=${ARCH})} ==="

# Find the image file
IMAGE_FILE="$(find "$IMAGE_DIR" -name "${IMAGE_NAME}*.raw" | head -1)"
if [[ -z "$IMAGE_FILE" ]]; then
    echo "ERROR: No image file found for ${IMAGE_NAME} in ${IMAGE_DIR}/"
    exit 1
fi
echo "Image file: ${IMAGE_FILE} ($(du -h "$IMAGE_FILE" | awk '{print $1}'))"

# Mount the image and inspect contents
MOUNT_DIR="$(mktemp -d)"
LOOP_DEV=""
cleanup() {
    sudo umount "$MOUNT_DIR" 2>/dev/null || true
    [[ -n "$LOOP_DEV" ]] && sudo losetup -d "$LOOP_DEV" 2>/dev/null || true
    rmdir "$MOUNT_DIR" 2>/dev/null || true
}
trap cleanup EXIT

# Find the root partition and mount it.
# Layout (amd64): p1=ESP (vfat), p2=BIOS boot (no fs, 1M), p3=root (ext4).
# Layout (arm64): p1=ESP (vfat), p2=root (ext4) — no BIOS boot partition.
# Detect the ext4 partition dynamically so this keeps working as the layout evolves.
LOOP_DEV="$(sudo losetup --find --show --partscan "$IMAGE_FILE")"
ROOT_PART=""
for part in "${LOOP_DEV}"p*; do
    [[ -b "$part" ]] || continue
    if sudo blkid -o value -s TYPE "$part" 2>/dev/null | grep -qx 'ext4'; then
        ROOT_PART="$part"
        break
    fi
done

if [[ -z "$ROOT_PART" ]]; then
    echo "ERROR: no ext4 root partition found on ${LOOP_DEV}"
    exit 1
fi

sudo mount -o ro "$ROOT_PART" "$MOUNT_DIR"

ERRORS=0

check_file() {
    local path="$1"
    local desc="$2"
    if [[ -f "$MOUNT_DIR/$path" ]] || [[ -x "$MOUNT_DIR/$path" ]]; then
        echo "  ✓ ${desc}: $(ls -la "$MOUNT_DIR/$path" | awk '{print $5, $NF}')"
    else
        echo "  ✗ ${desc}: MISSING ($path)"
        ERRORS=$((ERRORS + 1))
    fi
}

check_dir() {
    local path="$1"
    local desc="$2"
    if [[ -d "$MOUNT_DIR/$path" ]]; then
        echo "  ✓ ${desc}: exists"
    else
        echo "  ✗ ${desc}: MISSING ($path)"
        ERRORS=$((ERRORS + 1))
    fi
}

echo ""
echo "--- Kubernetes Binaries ---"
check_file "usr/local/bin/kubelet" "kubelet"
check_file "usr/local/bin/kubeadm" "kubeadm"
check_file "usr/local/bin/kubectl" "kubectl"
check_file "usr/local/bin/crictl" "crictl"

echo ""
echo "--- Container Runtime ---"
check_file "usr/local/bin/containerd" "containerd"
check_file "usr/local/sbin/runc" "runc"
check_file "etc/containerd/config.toml" "containerd config"

echo ""
echo "--- CNI Plugins ---"
check_dir "opt/cni/bin" "CNI bin directory"
if [[ -d "$MOUNT_DIR/opt/cni/bin" ]]; then
    CNI_COUNT="$(find "$MOUNT_DIR/opt/cni/bin" -type f | wc -l)"
    echo "  ✓ CNI plugins: ${CNI_COUNT} binaries found"
fi

echo ""
echo "--- System Configuration ---"
check_file "etc/modules-load.d/kubernetes.conf" "kernel modules config"
check_file "etc/sysctl.d/99-kubernetes.conf" "sysctl config"
check_file "usr/lib/systemd/system/kubelet.service" "kubelet service unit"
check_file "usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf" "kubeadm drop-in"
check_file "etc/crictl.yaml" "crictl config"

echo ""
echo "--- Kubernetes Version ---"
if [[ -f "$MOUNT_DIR/etc/kubernetes-version" ]]; then
    echo "  ✓ Kubernetes version: $(cat "$MOUNT_DIR/etc/kubernetes-version")"
else
    echo "  ✗ Version file missing"
    ERRORS=$((ERRORS + 1))
fi

if [[ -n "$ARCH" ]]; then
    echo ""
    echo "--- Binary Architecture (expected: ${ARCH}) ---"
    case "$ARCH" in
        amd64) EXPECTED_ELF="x86-64" ;;
        arm64) EXPECTED_ELF="ARM aarch64" ;;
        *) EXPECTED_ELF="" ;;
    esac
    if [[ -n "$EXPECTED_ELF" ]] && command -v file >/dev/null 2>&1; then
        for bin in usr/local/bin/kubelet usr/local/bin/containerd usr/local/sbin/runc; do
            if [[ -f "$MOUNT_DIR/$bin" ]]; then
                FILE_INFO="$(file -b "$MOUNT_DIR/$bin")"
                if grep -qF "$EXPECTED_ELF" <<<"$FILE_INFO"; then
                    echo "  ✓ ${bin}: ${EXPECTED_ELF}"
                else
                    echo "  ✗ ${bin}: expected '${EXPECTED_ELF}', got: ${FILE_INFO}"
                    ERRORS=$((ERRORS + 1))
                fi
            fi
        done
    else
        echo "  - skipped (file(1) not available or unknown arch)"
    fi
fi

echo ""
echo "--- Sysprep Checks ---"
if [[ -z "$(cat "$MOUNT_DIR/etc/machine-id" 2>/dev/null)" ]]; then
    echo "  ✓ machine-id: empty (will be regenerated on first boot)"
else
    echo "  ✗ machine-id: NOT empty"
    ERRORS=$((ERRORS + 1))
fi

SSH_KEYS="$(find "$MOUNT_DIR/etc/ssh" -name 'ssh_host_*' 2>/dev/null | wc -l)"
if [[ "$SSH_KEYS" -eq 0 ]]; then
    echo "  ✓ SSH host keys: removed"
else
    echo "  ✗ SSH host keys: ${SSH_KEYS} keys still present"
    ERRORS=$((ERRORS + 1))
fi

echo ""
if [[ "$ERRORS" -gt 0 ]]; then
    echo "FAILED: ${ERRORS} validation error(s)"
    exit 1
fi
echo "PASSED: All validation checks passed for ${IMAGE_NAME}"
