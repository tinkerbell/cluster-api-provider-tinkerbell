#!/bin/bash
# Push a built raw disk image to an OCI registry using oras.
# Usage: push-oci.sh --image-name <name> --kubernetes-version <ver> \
#                    --arch <amd64|arm64> \
#                    --repository <repo> --tag <tag>
set -euo pipefail

# ── Parse arguments ───────────────────────────────────────────────────────────
IMAGE_NAME=""
KUBERNETES_VERSION=""
ARCH=""
REPOSITORY=""
TAG=""
OUTPUT_DIR=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --image-name)         IMAGE_NAME="$2";         shift 2 ;;
        --kubernetes-version) KUBERNETES_VERSION="$2"; shift 2 ;;
        --arch)               ARCH="$2";               shift 2 ;;
        --repository)         REPOSITORY="$2";         shift 2 ;;
        --tag)                TAG="$2";                shift 2 ;;
        --output-dir)         OUTPUT_DIR="$2";         shift 2 ;;
        *) echo "Unknown argument: $1"; exit 1 ;;
    esac
done

# ── Validate ──────────────────────────────────────────────────────────────────
for var in IMAGE_NAME KUBERNETES_VERSION ARCH REPOSITORY TAG OUTPUT_DIR; do
    if [[ -z "${!var}" ]]; then
        echo "ERROR: --$(echo "$var" | tr '_' '-' | tr '[:upper:]' '[:lower:]') is required"
        exit 1
    fi
done

# ── Check for oras CLI ────────────────────────────────────────────────────────
if ! command -v oras &>/dev/null; then
    echo "ERROR: oras CLI not found. Install from https://oras.land/docs/installation"
    exit 1
fi

# ── Find the compressed image (require an unambiguous match) ─────────────────
mapfile -t IMAGE_FILES < <(find "$OUTPUT_DIR" -type f -name "${IMAGE_NAME}*.raw.gz")
if [[ "${#IMAGE_FILES[@]}" -eq 0 ]]; then
    echo "ERROR: No compressed image found for ${IMAGE_NAME} in ${OUTPUT_DIR}/"
    echo "       Run 'make build' first."
    exit 1
fi
if [[ "${#IMAGE_FILES[@]}" -gt 1 ]]; then
    echo "ERROR: Multiple compressed images found for ${IMAGE_NAME} in ${OUTPUT_DIR}/; pass an exact output directory or image name:"
    printf '  %s\n' "${IMAGE_FILES[@]}"
    exit 1
fi
IMAGE_FILE="${IMAGE_FILES[0]}"

IMAGE_REF="${REPOSITORY}:${TAG}"

ARTIFACT_TYPE="application/vnd.tinkerbell.image.raw.gz"

echo "==> Pushing image to OCI registry"
echo "    Image file:  ${IMAGE_FILE}"
echo "    Reference:   ${IMAGE_REF}"
echo "    Artifact:    ${ARTIFACT_TYPE}"
echo "    Image name:  ${IMAGE_NAME}"
echo "    Architecture: ${ARCH}"
echo "    Kubernetes:  ${KUBERNETES_VERSION}"
echo ""

# ── Push ──────────────────────────────────────────────────────────────────────
IMAGE_DIR="$(dirname "$IMAGE_FILE")"
IMAGE_BASENAME="$(basename "$IMAGE_FILE")"

pushd "$IMAGE_DIR" > /dev/null
oras push "$IMAGE_REF" \
    --artifact-type "$ARTIFACT_TYPE" \
    --annotation "org.opencontainers.image.title=${IMAGE_NAME}-k8s-${KUBERNETES_VERSION}" \
    --annotation "org.opencontainers.image.description=Kubernetes ${KUBERNETES_VERSION} node image (${IMAGE_NAME}, ${ARCH}) with containerd" \
    --annotation "io.tinkerbell.image.name=${IMAGE_NAME}" \
    --annotation "io.tinkerbell.image.kubernetes-version=${KUBERNETES_VERSION}" \
    --annotation "io.tinkerbell.image.architecture=${ARCH}" \
    --annotation "io.tinkerbell.image.runtime=containerd" \
    "$IMAGE_BASENAME"
popd > /dev/null

echo ""
echo "==> Successfully pushed: ${IMAGE_REF}"
echo "    Pull with: oras pull ${IMAGE_REF}"
