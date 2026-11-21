#!/usr/bin/env bash
# Tests for hack/check-release-assets.sh and hack/sign-image.sh, the guards
# the release workflow runs before it signs, attests or publishes.
#
# docker and cosign are stubbed: the stub docker prints a fixture index, and
# the stub cosign prints its arguments. Needs bash, jq, sha256sum and base64.
#
# Usage:
#   test-release-scripts.sh

set -uo pipefail

hack=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "${work}"' EXIT

failures=0

# expect NAME WANT CMD...: WANT is "ok" for exit status 0, "fail" otherwise.
expect() {
	local name=$1 want=$2
	shift 2
	local rc=0
	"$@" >"${work}/out" 2>&1 || rc=$?
	if [[ ${want} == ok && ${rc} -eq 0 ]] || [[ ${want} == fail && ${rc} -ne 0 ]]; then
		echo "ok   ${name}"
	else
		echo "FAIL ${name} (exit ${rc}, want ${want})"
		sed 's/^/     /' "${work}/out"
		failures=$((failures + 1))
	fi
}

b64() { base64 | tr -d '\n'; }

## check-release-assets.sh

check="${hack}/check-release-assets.sh"

# Writes three assets to a fresh ${work}/rel and their checksums to ${sums}.
setup_assets() {
	rm -rf "${work}/rel"
	mkdir "${work}/rel"
	echo components >"${work}/rel/infrastructure-components.yaml"
	echo metadata >"${work}/rel/metadata.yaml"
	echo template >"${work}/rel/cluster-template.yaml"
	sums=$(cd "${work}/rel" && sha256sum -- * | b64)
}

setup_assets
expect "assets: matching files pass" ok "${check}" "${work}/rel" "${sums}"

setup_assets
echo extra >"${work}/rel/extra.yaml"
expect "assets: an unlisted file fails" fail "${check}" "${work}/rel" "${sums}"

setup_assets
echo hidden >"${work}/rel/.hidden"
expect "assets: an unlisted hidden file fails" fail "${check}" "${work}/rel" "${sums}"

setup_assets
mkdir "${work}/rel/sub"
expect "assets: a subdirectory fails" fail "${check}" "${work}/rel" "${sums}"

setup_assets
rm "${work}/rel/metadata.yaml"
expect "assets: a missing file fails" fail "${check}" "${work}/rel" "${sums}"

setup_assets
echo tampered >>"${work}/rel/metadata.yaml"
expect "assets: a modified file fails" fail "${check}" "${work}/rel" "${sums}"

setup_assets
expect "assets: empty checksums fail" fail "${check}" "${work}/rel" ""

setup_assets
expect "assets: a malformed line fails" fail "${check}" "${work}/rel" "$(echo "deadbeef  metadata.yaml" | b64)"

setup_assets
expect "assets: a path outside the directory fails" fail "${check}" "${work}/rel" \
	"$(printf '%064d  ../metadata.yaml\n' 0 | b64)"

## sign-image.sh

sign="${hack}/sign-image.sh"

mkdir "${work}/bin"
cat >"${work}/bin/docker" <<'EOF'
#!/usr/bin/env bash
cat "${FIXTURE}"
EOF
cat >"${work}/bin/cosign" <<'EOF'
#!/usr/bin/env bash
echo "cosign $*"
EOF
chmod +x "${work}/bin/docker" "${work}/bin/cosign"

amd64=sha256:$(printf '%064d' 1)
arm64=sha256:$(printf '%064d' 2)
attestation=sha256:$(printf '%064d' 3)
printf '{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":"%s"},{"digest":"%s"},{"digest":"%s","annotations":{"vnd.docker.reference.type":"attestation-manifest"}}]}' \
	"${amd64}" "${arm64}" "${attestation}" >"${work}/index.json"
printf '{"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[]}' >"${work}/manifest.json"
index_digest=sha256:$(sha256sum "${work}/index.json" | cut -d' ' -f1)
manifest_digest=sha256:$(sha256sum "${work}/manifest.json" | cut -d' ' -f1)

# run_sign FIXTURE ARGS...: runs sign-image.sh with the stubs and an OIDC token.
run_sign() {
	local fixture=$1
	shift
	PATH="${work}/bin:${PATH}" FIXTURE="${fixture}" \
		ACTIONS_ID_TOKEN_REQUEST_URL=https://example.invalid ACTIONS_ID_TOKEN_REQUEST_TOKEN=token \
		"${sign}" "$@"
}

expect "sign: the index digest passes" ok run_sign "${work}/index.json" img v1.0.0 "${index_digest}"
want="cosign sign --yes img@${index_digest} img@${amd64} img@${arm64}"
if [[ "$(cat "${work}/out")" == "${want}" ]]; then
	echo "ok   sign: signs the index and each platform, not the attestations"
else
	echo "FAIL sign: signed refs: $(cat "${work}/out"), want: ${want}"
	failures=$((failures + 1))
fi

expect "sign: no OIDC token fails" fail env -u ACTIONS_ID_TOKEN_REQUEST_URL -u ACTIONS_ID_TOKEN_REQUEST_TOKEN \
	PATH="${work}/bin:${PATH}" FIXTURE="${work}/index.json" "${sign}" img v1.0.0 "${index_digest}"
expect "sign: a digest the tag doesn't resolve to fails" fail run_sign "${work}/index.json" img v1.0.0 "sha256:$(printf '%064d' 9)"
expect "sign: a malformed digest fails" fail run_sign "${work}/index.json" img v1.0.0 deadbeef
expect "sign: a single-platform manifest fails" fail run_sign "${work}/manifest.json" img v1.0.0 "${manifest_digest}"
expect "sign: missing arguments fail" fail run_sign "${work}/index.json" img "${index_digest}"

if [[ ${failures} -ne 0 ]]; then
	echo "${failures} test(s) failed"
	exit 1
fi
echo "all tests passed"
