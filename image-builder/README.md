# Kubernetes CAPI Node Image Builder (mkosi)

Build bootable raw disk images for Kubernetes Cluster API nodes using
[mkosi](https://github.com/systemd/mkosi) — a declarative, fast, and
reproducible alternative to Packer + Ansible.

Designed for [Tinkerbell](https://tinkerbell.org/) bare-metal provisioning.

## Features

- **Declarative** — INI-style mkosi config, no imperative Ansible playbooks
- **Fast** — Native package manager installation, no VM boot cycle
- **Ubuntu** — Ubuntu 24.04 LTS with containerd
- **Multi-arch** — Builds amd64 (BIOS+EFI) and arm64 (EFI-only) images
- **Reproducible** — Deterministic partition UUIDs, pinnable package versions
- **OCI distribution** — Push images to any OCI registry with `oras`
- **CI-ready** — Runs in containers, no nested virtualization needed

## Quick Start

### Prerequisites

- [mkosi](https://github.com/systemd/mkosi) v25+
- `systemd-repart`, `grub`, `dosfstools`, `e2fsprogs`, `mtools`
- `curl` (for downloading K8s binaries)
- `oras` (for pushing to OCI registries, optional)

### Build an Image

```bash
# Build Ubuntu 24.04 with containerd and Kubernetes v1.35.2 (defaults; ARCH defaults to host arch)
make build

# Build with a specific Kubernetes version
make build KUBERNETES_VERSION=v1.32.0

# Build for arm64 (requires arm64 host or QEMU/binfmt for cross-build)
make build ARCH=arm64

# Rebuild an existing image
make build FORCE=1

# Show supported Ubuntu versions
make versions
```

### Containerized Build (no host dependencies)

```bash
# Build the builder container image (per-arch tag)
make builder-image

# Build inside the container (matches host arch by default)
make build-containerized KUBERNETES_VERSION=v1.35.2
```

> **Note:** Cross-arch builds via QEMU/binfmt are **not currently
> supported**. mkosi installs seccomp filters that cannot be loaded under
> qemu-user emulation (`seccomp_load: Operation canceled`). To build an
> arm64 image, run on an arm64 host (native or `ubuntu-24.04-arm` GitHub
> runner). The CI matrix uses native runners for this reason.

### Push to OCI Registry

```bash
# Login to registry
make login OCI_REGISTRY=ghcr.io

# Build and push (image is pushed to $OCI_REGISTRY/$OCI_REPOSITORY:$OCI_TAG)
make push \
    KUBERNETES_VERSION=v1.35.2 \
    OCI_REGISTRY=ghcr.io \
    OCI_REPOSITORY=myorg/image-builder/ubuntu

# Pull on the other side (default tag is <ubuntu-short>-<k8s-version>-<arch>.gz)
oras pull ghcr.io/myorg/image-builder/ubuntu:2404-v1.35.2-amd64.gz
oras pull ghcr.io/myorg/image-builder/ubuntu:2404-v1.35.2-arm64.gz
```

### Validate a Built Image

```bash
make validate KUBERNETES_VERSION=v1.35.2
```

### Clean Up

```bash
make clean        # Remove build outputs for the current version
make clean-cache  # Remove the package cache
make clean-all    # Remove both
```

## Configuration

All configuration is via Make variables:

| Variable | Default | Description |
|---|---|---|
| `KUBERNETES_VERSION` | `v1.35.2` | Kubernetes version to install |
| `UBUNTU_VERSION` | `24.04` | Ubuntu version (`make versions` to list) |
| `ARCH` | host arch | Target CPU architecture: `amd64` or `arm64` |
| `FORCE` | (unset) | Set to `1` to rebuild existing images |
| `CNI_VERSION` | `v1.7.1` | CNI plugins version |
| `CRICTL_VERSION` | `v1.35.0` | crictl version |
| `CONTAINERD_VERSION` | `2.2.2` | containerd version |
| `RUNC_VERSION` | `v1.2.6` | runc version |
| `PAUSE_IMAGE` | `registry.k8s.io/pause:3.10` | Pause container image |
| `OCI_REGISTRY` | `ghcr.io` | OCI registry hostname |
| `OCI_REPOSITORY` | `tinkerbell/cluster-api-provider-tinkerbell/ubuntu` | OCI repository path (appended to `OCI_REGISTRY`) |
| `OCI_TAG` | `<ubuntu-short>-<k8s-version>-<arch>.gz` (e.g. `2404-v1.35.2-amd64.gz`) | OCI image tag |
| `BUILDER_IMAGE` | `k8s-image-builder` | Local tag for the containerized builder image |

## Supported Ubuntu Versions

| Version | Codename | Image ID (per arch)              |
|---------|----------|----------------------------------|
| 24.04   | Noble    | `k8s-node-ubuntu-2404-<arch>`    |

> **Note:** Ubuntu 24.10+ is not supported. 24.10 is EOL, and 25.04+ has
> `systemd-repart` merged into the `systemd` package, which is incompatible
> with mkosi v26.

## Architecture Support

The builder produces a separate disk image per CPU architecture, selected
at build time with `ARCH=amd64|arm64`.

| Arch  | Boot mode             | Bootloader packages                           |
|-------|-----------------------|-----------------------------------------------|
| amd64 | BIOS + UEFI           | `grub-pc-bin`, `grub-efi-amd64-bin`, `shim-signed` |
| arm64 | UEFI only             | `grub-efi-arm64-bin`, `shim-signed`           |

Output layout per build:

```
mkosi.output/<ubuntu-version>/<k8s-version>/<arch>/k8s-node-ubuntu-<short>-<arch>.raw[.gz]
```

OCI tags are per-arch (no manifest index): consumers select the
appropriate tag explicitly, e.g. `:2404-v1.35.2-amd64.gz` or
`:2404-v1.35.2-arm64.gz`. Each push carries an
`io.tinkerbell.image.architecture` annotation.
## Project Structure

```
├── mkosi.conf                      # Global build config (Ubuntu, containerd)
├── mkosi.conf.d/
│   ├── 10-packages.conf            # Common Ubuntu package list
│   ├── 20-amd64.conf               # amd64 drop-in (BIOS bootloader, repart)
│   └── 20-arm64.conf               # arm64 drop-in (EFI-only bootloader)
├── mkosi.repart/                   # Common partition layout (ESP + root)
├── mkosi.repart.amd64/             # amd64-only extra partitions (BIOS boot)
├── mkosi.extra/                    # Static overlay files
│   └── etc/, usr/lib/systemd/...   # sysctl, modules, kubelet unit
├── mkosi.prepare                   # Download K8s binaries + containerd
├── mkosi.postinst                  # Post-install configuration
├── mkosi.finalize                  # Sysprep/seal the image
├── mkosi.postoutput                # Compress + checksum
├── scripts/
│   ├── push-oci.sh                 # Push to OCI registry
│   └── validate.sh                 # Image validation
├── Makefile                        # Build orchestration
└── Containerfile                   # Containerized build env
```

## What Gets Installed

Each image includes:

- **Kubernetes**: kubelet, kubeadm, kubectl (from `dl.k8s.io`)
- **Container runtime**: containerd + runc (from upstream releases)
- **CNI plugins**: Standard CNI plugins (from GitHub releases)
- **crictl**: CRI CLI tool
- **cloud-init**: For first-boot provisioning
- **Networking**: conntrack, socat, ebtables, iptables, iproute2
- **Kernel config**: br_netfilter + overlay modules, IP forwarding sysctl
- **kubelet service**: systemd unit with kubeadm drop-in

## Architecture

```
                  mkosi.conf (global)
                       │
          ┌────────────┼────────────┐
          │            │            │
    mkosi.conf.d/  mkosi.repart/  mkosi.extra/
    (packages)     (partitions)   (overlay files)
          │
    ┌─────┼─────┬──────────┐
    │     │     │          │
 prepare postinst finalize postoutput
 (download) (config) (sysprep) (compress)
          │
       raw disk image (.raw.gz)
          │
    oras push → OCI registry
```

## License

Apache License 2.0
