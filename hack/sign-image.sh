#!/usr/bin/env bash
# Sign a multi-platform container image with cosign keyless signing.
#
# Signs the index and each platform manifest by digest, never by tag, so a
# reference pinned to either one verifies. BuildKit's attestation manifests,
# which hold the SBOM and the provenance, are not signed on their own: the
# index lists them by digest, so its signature covers them.
#
# The digest comes from GoReleaser's dist/artifacts.json, which falls back to
# the image config digest when buildx records no manifest digest. So the
# script fetches the index the registry serves for the tag and checks its
# bytes against the digest: a mismatch means the digest is not the one the
# tag resolves to, or the tag moved.
#
# The signature carries the identity of whoever signs. In GitHub Actions with
# `id-token: write`, that is this repository's workflow. Without that OIDC
# token, cosign signs with whatever identity it finds instead (another CI's
# ambient token, or an interactive login) and pushes that signature to the
# registry, so the script refuses to run without it.
#
# Usage:
#   sign-image.sh IMAGE TAG DIGEST
#
# Arguments:
#   IMAGE   Image name without tag or digest
#           (e.g. ghcr.io/tinkerbell/cluster-api-provider-tinkerbell).
#   TAG     The tag the release pushed (e.g. v0.7.0).
#   DIGEST  Digest of the pushed index.

set -euo pipefail

# cosign detects GitHub Actions through these two variables, which the runner
# sets only for jobs with `id-token: write`.
if [[ -z "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" || -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]]; then
	echo "$0: no GitHub Actions OIDC token (needs id-token: write); refusing to sign" >&2
	exit 1
fi

if [[ $# -ne 3 ]]; then
	echo "usage: $0 IMAGE TAG DIGEST" >&2
	exit 2
fi
image=$1
tag=$2
digest=$3

if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
	echo "::error::malformed image digest for ${image}: '${digest}'"
	exit 1
fi

# Hash a file: command substitution would strip trailing newlines.
index_file=$(mktemp)
trap 'rm -f "${index_file}"' EXIT
docker buildx imagetools inspect --raw "${image}:${tag}" >"${index_file}"
if [[ "sha256:$(sha256sum <"${index_file}" | cut -d' ' -f1)" != "${digest}" ]]; then
	echo "::error::${image}:${tag} does not resolve to ${digest}"
	exit 1
fi

media_type=$(jq -r '.mediaType' "${index_file}")
case "${media_type}" in
application/vnd.oci.image.index.v1+json | application/vnd.docker.distribution.manifest.list.v2+json) ;;
*)
	echo "::error::${image}@${digest} is not an image index: ${media_type}"
	exit 1
	;;
esac

# Not mapfile with process substitution: errexit would not see a jq failure.
platform_digests=$(jq -r '.manifests[] | select(.annotations["vnd.docker.reference.type"] != "attestation-manifest") | .digest' "${index_file}")

refs=("${image}@${digest}")
for platform_digest in ${platform_digests}; do
	if [[ ! "${platform_digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
		echo "::error::unexpected platform manifest digest in ${image}@${digest}: ${platform_digest}"
		exit 1
	fi
	refs+=("${image}@${platform_digest}")
done
if [[ ${#refs[@]} -lt 2 ]]; then
	echo "::error::no platform manifests in ${image}@${digest}"
	exit 1
fi

cosign sign --yes "${refs[@]}"
