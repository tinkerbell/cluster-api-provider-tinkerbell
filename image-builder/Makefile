# Kubernetes CAPI Node Image Builder (mkosi-based)
# Builds Ubuntu images with containerd for Tinkerbell bare-metal provisioning.
#
# Usage:
#   make build KUBERNETES_VERSION=v1.35.2
#   make build UBUNTU_VERSION=24.04 ARCH=arm64
#   make push OCI_REGISTRY=ghcr.io OCI_REPOSITORY=myorg/image-builder/ubuntu

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ── User-facing configuration ─────────────────────────────────────────────────
KUBERNETES_VERSION ?= v1.35.2
UBUNTU_VERSION     ?= 24.04
# Target CPU architecture: amd64 or arm64. Defaults to the host architecture.
ARCH               ?= $(shell uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)
OCI_REGISTRY       ?= ghcr.io
OCI_REPOSITORY     ?= tinkerbell/cluster-api-provider-tinkerbell/ubuntu
OCI_TAG            ?= $(UBUNTU_SHORT)-$(KUBERNETES_VERSION)-$(ARCH).gz

# ── Internal component versions ───────────────────────────────────────────────
CNI_VERSION        ?= v1.7.1
CRICTL_VERSION     ?= v1.35.0
CONTAINERD_VERSION ?= 2.2.2
RUNC_VERSION       ?= v1.2.6
PAUSE_IMAGE        ?= registry.k8s.io/pause:3.10

# ── Ubuntu version → codename mapping ─────────────────────────────────────────
UBUNTU_CODENAME_24.04 := noble
# 24.10+ not supported: 24.10 is EOL, 25.04+ has systemd-repart merged into systemd (incompatible with mkosi v26)

UBUNTU_RELEASE := $(UBUNTU_CODENAME_$(UBUNTU_VERSION))
ifeq ($(UBUNTU_RELEASE),)
$(error Unsupported UBUNTU_VERSION=$(UBUNTU_VERSION); supported: 24.04)
endif

# ── Architecture validation & mkosi mapping ───────────────────────────────────
ifeq ($(filter $(ARCH),amd64 arm64),)
$(error Unsupported ARCH=$(ARCH); supported values: amd64, arm64)
endif
# mkosi uses "x86-64" / "arm64" for --architecture
MKOSI_ARCH_amd64 := x86-64
MKOSI_ARCH_arm64 := arm64
MKOSI_ARCH := $(MKOSI_ARCH_$(ARCH))

# ── Derived values ────────────────────────────────────────────────────────────
UBUNTU_SHORT := $(subst .,,$(UBUNTU_VERSION))
IMAGE_ID     := k8s-node-ubuntu-$(UBUNTU_SHORT)-$(ARCH)

# ── Container build settings ──────────────────────────────────────────────────
BUILDER_IMAGE     ?= k8s-image-builder
# The builder image is host-arch native. Cross-arch builds (ARCH != host) use
# apt foreign-arch install + qemu-user-static/binfmt for postinst scripts.
# This is EXPERIMENTAL and not CI-tested; native runners per arch are recommended.
CONTAINER_RUNTIME ?= docker

# ── Directories ───────────────────────────────────────────────────────────────
OUTPUT_DIR  := mkosi.output/$(UBUNTU_VERSION)/$(KUBERNETES_VERSION)/$(ARCH)
CACHE_DIR   := mkosi.cache
SCRIPTS_DIR := scripts

# ── mkosi command and flags ───────────────────────────────────────────────────
MKOSI := mkosi $(if $(FORCE),--force)

MKOSI_ENV := \
	--environment=KUBERNETES_VERSION=$(KUBERNETES_VERSION) \
	--environment=ARCH=$(ARCH) \
	--environment=CNI_VERSION=$(CNI_VERSION) \
	--environment=CRICTL_VERSION=$(CRICTL_VERSION) \
	--environment=CONTAINERD_VERSION=$(CONTAINERD_VERSION) \
	--environment=RUNC_VERSION=$(RUNC_VERSION) \
	--environment=PAUSE_IMAGE=$(PAUSE_IMAGE)

MKOSI_SETTINGS := \
	--architecture=$(MKOSI_ARCH) \
	--release=$(UBUNTU_RELEASE) \
	--image-id=$(IMAGE_ID) \
	--output-directory=$(OUTPUT_DIR)


##@ Building

.PHONY: build
build: ## Build Ubuntu image with containerd
	$(MKOSI) $(MKOSI_ENV) $(MKOSI_SETTINGS) build

##@ Publishing

.PHONY: push
push: ## Build and push image to OCI registry
	@$(SCRIPTS_DIR)/push-oci.sh \
		--image-name $(IMAGE_ID) \
		--kubernetes-version $(KUBERNETES_VERSION) \
		--arch $(ARCH) \
		--repository $(OCI_REGISTRY)/$(OCI_REPOSITORY) \
		--tag $(OCI_TAG) \
		--output-dir $(OUTPUT_DIR)

.PHONY: login
login: ## Log in to OCI registry
	oras login $(OCI_REGISTRY)

##@ Containerized

.PHONY: builder-image
builder-image: ## Build the containerized build environment (host-arch)
	$(CONTAINER_RUNTIME) build -t $(BUILDER_IMAGE) -f Containerfile .

.PHONY: build-containerized
build-containerized: builder-image ## Build image inside container (cross-arch experimental; native recommended)
	$(CONTAINER_RUNTIME) run --rm --privileged \
		-v $$(pwd):/work:z \
		-v $$(pwd)/$(OUTPUT_DIR):/work/$(OUTPUT_DIR):z \
		-w /work \
		-e KUBERNETES_VERSION=$(KUBERNETES_VERSION) \
		-e ARCH=$(ARCH) \
		-e CNI_VERSION=$(CNI_VERSION) \
		-e CRICTL_VERSION=$(CRICTL_VERSION) \
		-e CONTAINERD_VERSION=$(CONTAINERD_VERSION) \
		-e RUNC_VERSION=$(RUNC_VERSION) \
		-e PAUSE_IMAGE=$(PAUSE_IMAGE) \
		$(BUILDER_IMAGE) \
		$(if $(FORCE),--force) \
		$(MKOSI_ENV) \
		$(MKOSI_SETTINGS) \
		build
	$(MAKE) fix-perms

##@ Validation

.PHONY: validate
validate: ## Validate the built image
	@$(SCRIPTS_DIR)/validate.sh --image-name $(IMAGE_ID) --output-dir $(OUTPUT_DIR) --arch $(ARCH)

##@ Permissions

.PHONY: fix-perms
fix-perms: ## Fix output file ownership to current user (no-op if already owned by $$USER)
	@if [ ! -d "$(OUTPUT_DIR)" ]; then \
		echo "==> fix-perms: $(OUTPUT_DIR) does not exist; skipping"; \
	elif [ "$$(stat -c %u "$(OUTPUT_DIR)")" = "$$(id -u)" ]; then \
		echo "==> fix-perms: $(OUTPUT_DIR) already owned by uid $$(id -u); skipping"; \
	else \
		echo "==> fix-perms: chowning $(OUTPUT_DIR) to $$(id -u):$$(id -g) via $(CONTAINER_RUNTIME)"; \
		$(CONTAINER_RUNTIME) run --rm -v $$(pwd)/$(OUTPUT_DIR):/out alpine chown -R $$(id -u):$$(id -g) /out; \
	fi

##@ Cleanup

.PHONY: clean
clean: ## Remove build outputs
	rm -rf $(OUTPUT_DIR)

.PHONY: clean-cache
clean-cache: ## Remove package cache
	rm -rf $(CACHE_DIR) mkosi.pkgcache

.PHONY: clean-all
clean-all: clean clean-cache ## Remove all build outputs and caches

##@ Help

.PHONY: versions
versions: ## Show supported Ubuntu versions
	@echo ""
	@echo "Supported Ubuntu versions (UBUNTU_VERSION=):"
	@echo ""
	@echo "  Version   Codename    Image ID"
	@echo "  ───────   ────────    ────────────────────────"
	@echo "  24.04     noble       k8s-node-ubuntu-2404"
	@echo ""

.PHONY: archs
archs: ## Show supported target architectures
	@echo ""
	@echo "Supported target architectures (ARCH=):"
	@echo ""
	@echo "  ARCH    mkosi name   Boot mode      Bootloader packages"
	@echo "  ─────   ──────────   ────────────   ──────────────────────────────────────────────"
	@echo "  amd64   x86-64       BIOS + UEFI    grub-pc-bin, grub-efi-amd64-bin, shim-signed"
	@echo "  arm64   arm64        UEFI only      grub-efi-arm64-bin, shim-signed"
	@echo ""
	@echo "  Default: host architecture (currently: $(ARCH))"
	@echo "  Output:  mkosi.output/<UBUNTU_VERSION>/<KUBERNETES_VERSION>/<ARCH>/"
	@echo "  OCI tag: <ubuntu-short>-<k8s-version>-<arch>.gz (e.g. 2404-v1.35.2-arm64.gz)"
	@echo ""

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m [VAR=value ...]\n"} \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""
	@echo "Variables:"
	@echo "  KUBERNETES_VERSION  Kubernetes version to install  (default: $(KUBERNETES_VERSION))"
	@echo "  UBUNTU_VERSION      Ubuntu version to build        (default: $(UBUNTU_VERSION))"
	@echo "  ARCH                Target architecture            (default: $(ARCH); one of amd64, arm64; see 'make archs')"
	@echo "  FORCE               Set to 1 to rebuild existing   (default: unset)"
	@echo "  CNI_VERSION         CNI plugins version            (default: $(CNI_VERSION))"
	@echo "  CRICTL_VERSION      crictl version                 (default: $(CRICTL_VERSION))"
	@echo "  CONTAINERD_VERSION  containerd version             (default: $(CONTAINERD_VERSION))"
	@echo "  RUNC_VERSION        runc version                   (default: $(RUNC_VERSION))"
	@echo "  PAUSE_IMAGE         Pause container image          (default: $(PAUSE_IMAGE))"
	@echo "  OCI_REGISTRY        OCI registry hostname          (default: $(OCI_REGISTRY))"
	@echo "  OCI_REPOSITORY      Full OCI repository path       (default: $(OCI_REPOSITORY))"
	@echo "  OCI_TAG             OCI image tag                  (default: $(OCI_TAG))"
	@echo "  BUILDER_IMAGE       Local tag for the builder img  (default: $(BUILDER_IMAGE))"
	@echo "  CONTAINER_RUNTIME   docker or podman               (default: $(CONTAINER_RUNTIME))"
