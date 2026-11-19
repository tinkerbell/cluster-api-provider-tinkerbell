#!/usr/bin/env bash
# Check downloaded release assets against the checksums the job that built
# them output.
#
# Artifacts carry the release assets between jobs, but any job in the run can
# replace an artifact; only the job that built the assets can set its outputs.
# So the jobs that attest or publish the assets check them against those
# outputs, never against checksums that came with the download. The directory
# must hold exactly the files the checksums name, so the jobs can publish the
# whole directory.
#
# Usage:
#   check-release-assets.sh DIR SHA256SUMS
#
# Arguments:
#   DIR         The directory the assets were downloaded to.
#   SHA256SUMS  The base64 of `sha256sum` output for the files in DIR, named
#               without a directory, as the build job outputs it.

set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 DIR SHA256SUMS" >&2
	exit 2
fi
dir=$1
b64=$2

if [[ -z "${b64}" ]]; then
	echo "::error::the build output no checksums"
	exit 1
fi
sums=$(base64 --decode <<<"${b64}")

expected=()
hashes=()
while IFS= read -r line; do
	if [[ ! "${line}" =~ ^([0-9a-f]{64})\ \ ([A-Za-z0-9._-]+)$ ]]; then
		echo "::error::malformed checksum line from the build: '${line}'"
		exit 1
	fi
	hashes+=("${BASH_REMATCH[1]}")
	expected+=("${BASH_REMATCH[2]}")
done <<<"${sums}"

# The listing must match exactly: an extra file would be published unchecked.
actual=$(cd "${dir}" && find . -mindepth 1 | sed 's|^\./||' | LC_ALL=C sort)
listed=$(printf '%s\n' "${expected[@]}" | LC_ALL=C sort)
if [[ "${actual}" != "${listed}" ]]; then
	echo "::error::${dir} does not hold exactly the files the build listed"
	diff <(echo "${listed}") <(echo "${actual}") || true
	exit 1
fi

# Compare the hashes here rather than with sha256sum --check, whose options
# differ between GNU and macOS.
cd "${dir}"
for i in "${!expected[@]}"; do
	actual_hash=$(sha256sum <"${expected[i]}" | cut -d' ' -f1)
	if [[ "${actual_hash}" != "${hashes[i]}" ]]; then
		echo "::error::${expected[i]} does not match the checksum from the build"
		exit 1
	fi
	echo "${expected[i]}: OK"
done
