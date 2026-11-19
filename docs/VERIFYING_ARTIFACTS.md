# Verifying release artifacts

The release workflow signs the image and attests the release assets with keyless [Sigstore](https://www.sigstore.dev/) signatures tied to the workflow that built them. There are no keys to fetch.

| Artifact | Signed with | SBOM |
| --- | --- | --- |
| `ghcr.io/tinkerbell/cluster-api-provider-tinkerbell:<version>` | cosign | SPDX, attached to the image by BuildKit |
| Release assets: `infrastructure-components.yaml`, `metadata.yaml`, `cluster-template.yaml` | GitHub build provenance attestation | none |

Releases after `v0.7.0` are signed. `v0.7.0` and earlier releases are not.

Releases are signed by `https://github.com/tinkerbell/cluster-api-provider-tinkerbell/.github/workflows/release.yaml@refs/tags/<version>`. Check that identity, with the version you deploy.

The identity names the tag, not the commit. Release tags are never moved or reused, but if one were, artifacts from the earlier commit would still match the identity. So the examples also check the commit the tag points to now.

Only the version tags are signed. Builds of `main` push `:latest` and `:sha-<commit>` unsigned, and `:latest` moves between `main` builds and releases. Don't deploy them in production.

Requirements:

- [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) v3 or later. cosign v2.6 and later also work with `--new-bundle-format`.
- [gh](https://cli.github.com/), logged in with `gh auth login`, to look up the release commit and verify the release assets.
- `docker buildx`, to read the image SBOM and provenance.

The examples use:

```sh
VERSION=v0.8.0  # the release to check
COMMIT=$(gh api "repos/tinkerbell/cluster-api-provider-tinkerbell/commits/${VERSION}" --jq .sha)
```

## Container image

```sh
cosign verify "ghcr.io/tinkerbell/cluster-api-provider-tinkerbell:${VERSION}" \
  --certificate-identity "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/.github/workflows/release.yaml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-sha "${COMMIT}"
```

The multi-platform index and each platform manifest are signed, so a reference pinned to either digest verifies too.

A tag can move after you check it. `cosign verify` prints the index digest it checked:

```sh
DIGEST=$(cosign verify "ghcr.io/tinkerbell/cluster-api-provider-tinkerbell:${VERSION}" \
  --certificate-identity "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/.github/workflows/release.yaml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-sha "${COMMIT}" \
  | jq -r '.[0].critical.image."docker-manifest-digest"')
echo "ghcr.io/tinkerbell/cluster-api-provider-tinkerbell@${DIGEST}"
```

`infrastructure-components.yaml` refers to the image by tag, so that `clusterctl` image overrides keep working. To deploy the digest you verified, see [Installing the verified assets with clusterctl](#installing-the-verified-assets-with-clusterctl).

### SBOM and provenance

Read them from the index digest you verified, so they belong to that image. They hang off the index, so a platform manifest digest doesn't reach them: the commands then print `{}`.

```sh
docker buildx imagetools inspect "ghcr.io/tinkerbell/cluster-api-provider-tinkerbell@${DIGEST}" --format '{{ json .SBOM }}'
docker buildx imagetools inspect "ghcr.io/tinkerbell/cluster-api-provider-tinkerbell@${DIGEST}" --format '{{ json .Provenance }}'
```

The output holds one document per platform. `cosign verify-attestation` does not see them: BuildKit references them from the image index, which the image signature covers.

## Release assets

```sh
mkdir capt-${VERSION} && cd capt-${VERSION}
gh release download "${VERSION}" --repo tinkerbell/cluster-api-provider-tinkerbell

for asset in *.yaml; do
  gh attestation verify "${asset}" \
    --repo tinkerbell/cluster-api-provider-tinkerbell \
    --cert-identity "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/.github/workflows/release.yaml@refs/tags/${VERSION}" \
    --source-digest "${COMMIT}" \
    --deny-self-hosted-runners
done
```

### Installing the verified assets with clusterctl

`clusterctl init` downloads `infrastructure-components.yaml` and `metadata.yaml` from the GitHub release and does not verify them. To install the files you verified, put them in the clusterctl [overrides layer](https://cluster-api.sigs.k8s.io/clusterctl/configuration#overrides-layer):

```sh
OVERRIDES="${XDG_CONFIG_HOME:-${HOME}/.config}/cluster-api/overrides/infrastructure-tinkerbell/${VERSION}"
mkdir -p "${OVERRIDES}"
cp infrastructure-components.yaml metadata.yaml "${OVERRIDES}/"
clusterctl init --infrastructure "tinkerbell:${VERSION}"
```

To pin the image you verified, replace `ghcr.io/tinkerbell/cluster-api-provider-tinkerbell:${VERSION}` in the copied `infrastructure-components.yaml` with `ghcr.io/tinkerbell/cluster-api-provider-tinkerbell@${DIGEST}`.
