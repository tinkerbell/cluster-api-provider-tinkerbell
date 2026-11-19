# Release

This document describes how to release the Tinkerell infrastructure provider.

This is _not_ intended for regular users.

This is normally performed by our CI system. However, there are important steps to take first.

## How to Cut a Release

In order to cut a release, you must:

1. If this is a new major or minor version - but **not** just a patch change - update [metadata.yaml](./metadata.yaml) to add it, and map it to the correct cluster-api contract version
1. Commit the changes.
1. Push out your branch, open a PR and merge the changes
1. Wait for the Continuous Integration github action to finish running
1. Tag the release with `git tag -a vX.Y.z -m "Message"
1. Push out the tag. Never move, delete and re-push, or reuse a release tag: signatures name the tag, so artifacts from the old commit would still verify. Cut a new patch version instead.

## How A Release Happens

* GitHub Actions detects a new tag has been pushed
* CI builds docker images for each supported architecture as well as a multi-arch manifest, and tags it with the semver tag of the release, e.g. `v0.1.0`
* CI creates the release in `out/release`, the equivalent of `make release`
* CI signs the image with cosign and attests the artifacts in `out/release/*` with a GitHub build provenance attestation
* CI copies the artifacts in `out/release/*` to the github releases

Users verify a release as described in [Verifying release artifacts](./VERIFYING_ARTIFACTS.md).

The image is pushed with its version tag and `latest` before it is signed, and the GitHub release is published last. If signing fails (for example, a Sigstore outage), the registry serves an unsigned image under the version tag until it is signed. Re-run the failed jobs of the same workflow run: they sign the digest the build recorded and then publish the release. Don't push the tag again.
