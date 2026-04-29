#!/bin/bash
# Validate a built image by checking for required Kubernetes binaries and configuration.
# Usage: validate.sh --image-name <name> --output-dir <dir> [--arch <amd64|arm64>]
set -euo pipefail

usage() {
    echo "Usage: validate.sh --image-name <name> --output-dir <dir> [--arch <amd64|arm64>]" >&2
}

IMAGE_NAME=""
IMAGE_DIR=""
ARCH=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --image-name) IMAGE_NAME="$2"; shift 2 ;;
        --output-dir) IMAGE_DIR="$2";  shift 2 ;;
        --arch)       ARCH="$2";       shift 2 ;;
        -h|--help)    usage; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
    esac
done

if [[ -z "$IMAGE_NAME" || -z "$IMAGE_DIR" ]]; then
    echo "ERROR: --image-name and --output-dir are required" >&2
    usage
    exit 1
fi

echo "=== Validating ${IMAGE_NAME} image${ARCH:+ (arch=${ARCH})} ==="

# Find the image file and require an unambiguous match.
mapfile -t IMAGE_FILES < <(find "$IMAGE_DIR" -type f -name "${IMAGE_NAME}*.raw")
if [[ "${#IMAGE_FILES[@]}" -eq 0 ]]; then
    echo "ERROR: No image file found for ${IMAGE_NAME} in ${IMAGE_DIR}/"
    exit 1
fi
if [[ "${#IMAGE_FILES[@]}" -gt 1 ]]; then
    echo "ERROR: Multiple image files found for ${IMAGE_NAME} in ${IMAGE_DIR}/; pass an exact output directory or image name:"
    printf '  %s\n' "${IMAGE_FILES[@]}"
    exit 1
fi
IMAGE_FILE="${IMAGE_FILES[0]}"
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
    if [[ -f "$MOUNT_DIR/$path" ]]; then
        echo "  ✓ ${desc}: $(ls -la "$MOUNT_DIR/$path" | awk '{print $5, $NF}')"
    else
        echo "  ✗ ${desc}: MISSING ($path)"
        ERRORS=$((ERRORS + 1))
    fi
}

check_exec() {
    local path="$1"
    local desc="$2"
    if [[ ! -f "$MOUNT_DIR/$path" ]]; then
        echo "  ✗ ${desc}: MISSING ($path)"
        ERRORS=$((ERRORS + 1))
    elif [[ ! -x "$MOUNT_DIR/$path" ]]; then
        echo "  ✗ ${desc}: NOT EXECUTABLE ($path)"
        ERRORS=$((ERRORS + 1))
    else
        echo "  ✓ ${desc}: $(ls -la "$MOUNT_DIR/$path" | awk '{print $5, $NF}')"
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
check_exec "usr/local/bin/kubelet" "kubelet"
check_exec "usr/local/bin/kubeadm" "kubeadm"
check_exec "usr/local/bin/kubectl" "kubectl"
check_exec "usr/local/bin/crictl" "crictl"

echo ""
echo "--- Container Runtime ---"
check_exec "usr/local/bin/containerd" "containerd"
check_exec "usr/local/sbin/runc" "runc"
check_file "etc/containerd/config.toml" "containerd config"

echo ""
echo "--- CNI Plugins ---"
check_dir "opt/cni/bin" "CNI bin directory"
if [[ -d "$MOUNT_DIR/opt/cni/bin" ]]; then
    CNI_COUNT="$(find "$MOUNT_DIR/opt/cni/bin" -type f -executable | wc -l)"
    echo "  ✓ CNI plugins: ${CNI_COUNT} executable binaries found"
    # The upstream CNI plugins tarball ships non-binary files (e.g. LICENSE,
    # README.md) alongside the binaries. Surface them for visibility but do
    # not treat them as a validation failure.
    CNI_NONEXEC_LIST="$(find "$MOUNT_DIR/opt/cni/bin" -type f ! -executable -printf '%P\n' | sort)"
    if [[ -n "$CNI_NONEXEC_LIST" ]]; then
        echo "  - CNI plugins: non-executable files in opt/cni/bin (informational):"
        while IFS= read -r f; do
            echo "      $f"
        done <<< "$CNI_NONEXEC_LIST"
    fi
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
if [[ ! -f "$MOUNT_DIR/etc/machine-id" ]]; then
    echo "  ✗ machine-id: MISSING (expected present but empty)"
    ERRORS=$((ERRORS + 1))
elif [[ -s "$MOUNT_DIR/etc/machine-id" ]]; then
    echo "  ✗ machine-id: NOT empty"
    ERRORS=$((ERRORS + 1))
else
    echo "  ✓ machine-id: empty (will be regenerated on first boot)"
fi

if [[ -f "$MOUNT_DIR/etc/fstab" ]]; then
    SWAP_LINES="$(awk '!/^[[:space:]]*#/ && $3 == "swap"' "$MOUNT_DIR/etc/fstab")"
    if [[ -n "$SWAP_LINES" ]]; then
        echo "  ✗ fstab: swap entry still present:"
        printf '      %s\n' "$SWAP_LINES"
        ERRORS=$((ERRORS + 1))
    else
        echo "  ✓ fstab: no swap entries"
    fi
else
    echo "  - fstab: not present (skipped)"
fi

if [[ ! -d "$MOUNT_DIR/etc/ssh" ]]; then
    echo "  ✓ SSH host keys: /etc/ssh not present (no host keys)"
else
    SSH_KEY_FILES=()
    while IFS= read -r -d '' f; do
        SSH_KEY_FILES+=("$f")
    done < <(find "$MOUNT_DIR/etc/ssh" -name 'ssh_host_*' -print0 2>/dev/null)
    if [[ "${#SSH_KEY_FILES[@]}" -eq 0 ]]; then
        echo "  ✓ SSH host keys: removed"
    else
        echo "  ✗ SSH host keys: ${#SSH_KEY_FILES[@]} keys still present"
        ERRORS=$((ERRORS + 1))
    fi
fi

echo ""
if [[ "$ERRORS" -gt 0 ]]; then
    echo "FAILED: ${ERRORS} validation error(s)"
    exit 1
fi
echo "PASSED: All validation checks passed for ${IMAGE_NAME}"
