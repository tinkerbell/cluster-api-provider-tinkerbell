# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: vendor test manager clusterctl run install deploy crds generate fmt vet run kubebuilder ci cd

GIT_VERSION ?= $(shell git log -1 --format="%H")
RELEASE_TAG := $(shell git describe --abbrev=0 --tags ${TAG_COMMIT} 2>/dev/null || true)
RELEASE_VERSION ?= $(shell cat VERSION)

# are there uncommitted files?
ifneq ($(shell git status --porcelain),)
	# next is used by GoReleaser as well when --spanshot is set
  RELEASE_TAG := $(RELEASE_TAG)-next
  RELEASE_VERSION := $(RELEASE_VERSION)-dirty
endif

# this is being kept, as in the future, we may check the diff of VERSION from the previous to determine
# whether or not to cut a release, rather than relying on git tags
#ifeq ($(shell git diff HEAD~1 VERSION),)
#  RELEASE_VERSION := $(RELEASE_VERSION)-next
#endif

VERSION ?= $(RELEASE_VERSION)

# which arches can we support
ARCHES=arm64 amd64

QEMU_VERSION?=4.2.0-7
QEMU_IMAGE?=multiarch/qemu-user-static:$(QEMU_VERSION)

KUBEBUILDER_VERSION ?= 2.3.1
# default install location for kubebuilder; can be placed elsewhere
KUBEBUILDER_DIR ?= /usr/local/kubebuilder
KUBEBUILDER ?= $(KUBEBUILDER_DIR)/bin/kubebuilder

CERTMANAGER_URL ?= https://github.com/jetstack/cert-manager/releases/download/v0.14.1/cert-manager.yaml

REPO_URL ?= https://github.com/tinkerbell/cluster-api-provider-tinkerbell

# BUILDARCH is the host architecture
# ARCH is the target architecture
# we need to keep track of them separately
BUILDARCH ?= $(shell uname -m)
BUILDOS ?= $(shell uname -s | tr A-Z a-z)

# Allow overriding the imagePullPolicy
PULL_POLICY ?= Always

E2E_FOCUS := "functional tests"

# Directories.
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/bin
BIN_DIR := bin
TEST_DIR := test
TEST_E2E_DIR := $(TEST_DIR)/e2e
ENVSUBST_BIN := bin/envsubst
ENVSUBST := $(TOOLS_DIR)/$(ENVSUBST_BIN)

# Binaries.
KUSTOMIZE := $(abspath $(TOOLS_BIN_DIR)/kustomize)
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/controller-gen)
ENVSUBST := $(abspath $(TOOLS_BIN_DIR)/envsubst)

envsubst: $(ENVSUBST) ## Build a local copy of envsubst.
$(ENVSUBST): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -tags=tools -o $(ENVSUBST_BIN) github.com/drone/envsubst/cmd/envsubst

controller-gen: $(CONTROLLER_GEN) ## Build a local copy of controller-gen.
$(CONTROLLER_GEN): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -tags=tools -o $(BIN_DIR)/controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen

kustomize: $(KUSTOMIZE)
$(KUSTOMIZE): $(TOOLS_DIR)/go.mod # Build kustomize from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/kustomize sigs.k8s.io/kustomize/kustomize/v3

# canonicalized names for host architecture
ifeq ($(BUILDARCH),aarch64)
        BUILDARCH=arm64
endif
ifeq ($(BUILDARCH),x86_64)
        BUILDARCH=amd64
endif

# unless otherwise set, I am building for my own architecture, i.e. not cross-compiling
ARCH ?= $(BUILDARCH)

# canonicalized names for target architecture
ifeq ($(ARCH),aarch64)
        override ARCH=arm64
endif
ifeq ($(ARCH),x86_64)
    override ARCH=amd64
endif

# unless otherwise set, I am building for my own OS, i.e. not cross-compiling
OS ?= $(BUILDOS)

# Image URL to use all building/pushing image targets
BUILD_IMAGE ?= tinkerbell/cluster-api-provider-tinkerbell
BUILD_IMAGE_TAG ?= $(BUILD_IMAGE):latest
PUSH_IMAGE_TAG ?= $(BUILD_IMAGE):$(IMAGETAG)
MANAGER ?= bin/manager-$(OS)-$(ARCH)
KUBECTL ?= kubectl
FROMTAG ?= latest

IMAGENAME ?= $(BUILD_IMAGE):$(IMAGETAG)-$(ARCH)

# Manifest tool, until `docker manifest` is fully ready. As of this writing, it remains experimental
MANIFEST_VERSION ?= 1.0.0
MANIFEST_URL = https://github.com/estesp/manifest-tool/releases/download/v$(MANIFEST_VERSION)/manifest-tool-$(BUILDOS)-$(BUILDARCH)

# these macros create a list of valid architectures for pushing manifests
space :=
space +=
comma := ,
prefix_linux = $(addprefix linux/,$(strip $1))
join_platforms = $(subst $(space),$(comma),$(call prefix_linux,$(strip $1)))

GO ?= GO111MODULE=on CGO_ENABLED=0 go


# Image URL to use all building/pushing image targets
IMG ?= quay.io/tinkerbell/cluster-api-provider-tinkerbell:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

MANIFEST_TOOL ?= $(GOBIN)/manifest-tool

# where we store downloaded core
COREPATH ?= out/core
CORE_VERSION ?= v0.3.8
CORE_API ?= https://api.github.com/repos/kubernetes-sigs/cluster-api/releases
CORE_URL ?= https://github.com/kubernetes-sigs/cluster-api/releases/download/$(CORE_VERSION)

# metadata file to be included in releases
METADATA_YAML ?= metadata.yaml

# actual releases
RELEASE_BASE := out/release/infrastructure-tinkerbell
RELEASE_DIR := $(RELEASE_BASE)/$(RELEASE_VERSION)
FULL_RELEASE_DIR := $(realpath .)/$(RELEASE_DIR)
RELEASE_MANIFEST := $(RELEASE_DIR)/infrastructure-components.yaml
RELEASE_METADATA := $(RELEASE_DIR)/metadata.yaml
RELEASE_CLUSTER_TEMPLATE := $(RELEASE_DIR)/cluster-template.yaml
FULL_RELEASE_MANIFEST := $(FULL_RELEASE_DIR)/infrastructure-components.yaml
FULL_RELEASE_MANIFEST_URL := $(REPO_URL)/releases/$(RELEASE_VERSION)/infrastructure-components.yaml
FULL_RELEASE_CLUSTERCTLYAML := $(FULL_RELEASE_DIR)/clusterctl.yaml
RELEASE_CLUSTERCTLYAML := $(RELEASE_BASE)/clusterctl-$(RELEASE_VERSION).yaml

# managerless - for running manager locally for testing
MANAGERLESS_VERSION ?= $(RELEASE_VERSION)
MANAGERLESS_BASE := out/managerless/infrastructure-tinkerbell
MANAGERLESS_DIR := $(MANAGERLESS_BASE)/$(RELEASE_VERSION)
FULL_MANAGERLESS_DIR := $(realpath .)/$(MANAGERLESS_DIR)
MANAGERLESS_MANIFEST := $(MANAGERLESS_DIR)/infrastructure-components.yaml
MANAGERLESS_METADATA := $(MANAGERLESS_DIR)/metadata.yaml
MANAGERLESS_CLUSTER_TEMPLATE := $(MANAGERLESS_DIR)/cluster-template.yaml
FULL_MANAGERLESS_MANIFEST := $(FULL_MANAGERLESS_DIR)/infrastructure-components.yaml
MANAGERLESS_CLUSTERCTLYAML := $(MANAGERLESS_BASE)/clusterctl-$(MANAGERLESS_VERSION).yaml

# templates
CLUSTERCTL_TEMPLATE ?= templates/clusterctl-template.yaml
CLUSTER_TEMPLATE ?= templates/cluster-template.yaml


all: manager

# 2 separate targets: ci-test does everything locally, does not need docker; ci includes ci-test and building the image
ci: test image

imagetag:
ifndef IMAGETAG
	$(error IMAGETAG is undefined - run using make <target> IMAGETAG=X.Y.Z)
endif

tag-images-all: $(addprefix sub-tag-image-, $(ARCHES))
sub-tag-image-%:
	@$(MAKE) ARCH=$* IMAGETAG=$(IMAGETAG) tag-images

tag-image: imagetag
	docker tag $(BUILD_IMAGE_TAG) $(PUSH_IMAGE_TAG)
tag-images: imagetag
	docker tag $(BUILD_IMAGE):$(FROMTAG)-$(ARCH) $(IMAGENAME)

confirm:
ifndef CONFIRM
	$(error CONFIRM is undefined - run using make <target> CONFIRM=true)
endif

.PHONY: branchname
branchname:
ifndef BRANCH_NAME
	$(error BRANCH_NAME is undefined - run using make <target> BRANCH_NAME=var or set an environment variable)
endif

cd: confirm branchname
	$(MAKE) tag-images-all push-all push-manifest IMAGETAG=${BRANCH_NAME}
	$(MAKE) tag-images-all push-all push-manifest IMAGETAG=${GIT_VERSION}

# needed kubebuilder for tests
kubebuilder: $(KUBEBUILDER)
$(KUBEBUILDER):
	curl -sL https://go.kubebuilder.io/dl/$(KUBEBUILDER_VERSION)/$(BUILDOS)/$(BUILDARCH) | tar -xz -C /tmp/
	# move to a long-term location and put it on your path
	# (you'll need to set the KUBEBUILDER_ASSETS env var if you put it somewhere else)
	mv /tmp/kubebuilder_$(KUBEBUILDER_VERSION)_$(BUILDOS)_$(BUILDARCH) $(KUBEBUILDER_DIR)

# Run tests
test: generate fmt vet crds
	go test ./... -coverprofile cover.out

.PHONY: e2e-image
e2e-image:
	docker build --tag=quay.io/tinkerbell/cluster-api-provider-tinkerbell:e2e .

# Run e2e tests
.PHONY: e2e
e2e: e2e-image
	# This is the name used inside the component.yaml for the container that runs the manager
	# The image gets loaded inside kind from ./test/e2e/config/tinkerbell-dev.yaml
	$(E2E_FLAGS) $(MAKE) -C $(TEST_E2E_DIR) run

# Build manager binary
manager: $(MANAGER)
$(MANAGER): generate fmt vet
	GOOS=$(OS) GOARCH=$(ARCH) $(GO) build -o $@ .

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet crds
	go run ./main.go

# Install CRDs into a cluster
install: crds
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: crds
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: crds
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/release | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
crds: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		paths=./api/... \
		paths=./controllers/... \
		crd:crdVersions=v1 \
		rbac:roleName=manager-role \
		output:crd:dir=./config/crd/bases \
		output:webhook:dir=./config/webhook \
		webhook

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

## make the images for all supported ARCH
image-all: $(addprefix sub-image-, $(ARCHES))
sub-image-%:
	@$(MAKE) ARCH=$* image

# Build the docker image for a single arch
image: test
	docker buildx build --load -t $(IMG)-$(ARCH) -f Dockerfile --build-arg ARCH=$(ARCH) --platform $(OS)/$(ARCH) .
	echo "Done. image is at $(IMG)-$(ARCH)"

  # Targets used when cross building.
.PHONY: register
# Enable binfmt adding support for miscellaneous binary formats.
# This is only needed when running non-native binaries.
register:
	docker pull $(QEMU_IMAGE)
	docker run --rm --privileged $(QEMU_IMAGE) --reset -p yes || true

.PHONY: manifest-tool
manifest-tool: $(MANIFEST_TOOL)
$(MANIFEST_TOOL):
	curl -L -o $@ $(MANIFEST_URL)
	chmod +x $@

## push the multi-arch manifest
push-manifest: manifest-tool imagetag
	# path to credentials based on manifest-tool's requirements here https://github.com/estesp/manifest-tool#sample-usage
	$(GOBIN)/manifest-tool push from-args --platforms $(call join_platforms,$(ARCHES)) --template $(BUILD_IMAGE):$(IMAGETAG)-ARCH --target $(BUILD_IMAGE):$(IMAGETAG)

push-all: imagetag $(addprefix sub-push-, $(ARCHES))
sub-push-%:
	@$(MAKE) ARCH=$* push IMAGETAG=$(IMAGETAG)

# Push the docker image
push:
	docker push $(IMAGENAME)

## generate a cluster using clusterctl and setting defaults
cluster:
	./scripts/generate-cluster.sh

$(RELEASE_DIR) $(RELEASE_BASE):
	mkdir -p $@

$(MANAGERLESS_DIR) $(MANAGERLESS_BASE):
	mkdir -p $@

.PHONY: semver release-clusterctl release-manifests release $(RELEASE_CLUSTERCTLYAML) $(RELEASE_MANIFEST) $(RELEASE_METADATA) $(RELEASE_CLUSTER_TEMPLATE) $(FULL_RELEASE_CLUSTERCTLYAML)

semver:
ifeq (,$(VERSION))
	$(error could not determine version to use from file, will not create artifacts)
endif
	@echo "release version $(VERSION)"


manifest: kustomize semver release-manifests release-clusterctl release-cluster-template

release-manifests: semver $(RELEASE_MANIFEST) $(RELEASE_METADATA) $(RELEASE_CLUSTER_TEMPLATE)
release-version:
	KUSTOMIZE_ENABLE_ALPHA_COMMANDS=true $(KUSTOMIZE) cfg set config image-tag $(VERSION)

$(RELEASE_MANIFEST): $(RELEASE_DIR) release-version ## Builds the manifests to publish with a release
	$(KUSTOMIZE) build config > $@

$(RELEASE_METADATA): semver $(RELEASE_DIR)
	cp $(METADATA_YAML) $@

release-cluster-template: semver $(RELEASE_CLUSTER_TEMPLATE)
$(RELEASE_CLUSTER_TEMPLATE): $(RELEASE_DIR)
	cp $(CLUSTER_TEMPLATE) $@

release-clusterctl: semver $(RELEASE_CLUSTERCTLYAML) $(FULL_RELEASE_CLUSTERCTLYAML)
$(RELEASE_CLUSTERCTLYAML): $(RELEASE_BASE)
	cat $(CLUSTERCTL_TEMPLATE) | sed 's%URL%$(FULL_RELEASE_MANIFEST)%g' > $@

$(FULL_RELEASE_CLUSTERCTLYAML): $(RELEASE_DIR)
	cat $(CLUSTERCTL_TEMPLATE) | sed 's%URL%$(FULL_RELEASE_MANIFEST_URL)%g' > $@

.PHONY: managerless-clusterctl managerless-manifests managerless $(MANAGERLESS_CLUSTERCTLYAML) $(MANAGERLESS_MANIFEST) $(MANAGERLESS_METADATA) $(MANAGERLESS_CLUSTER_TEMPLATE)
managerless: semver managerless-manifests managerless-clusterctl managerless-cluster-template
managerless-manifests: semver $(MANAGERLESS_MANIFEST) $(MANAGERLESS_METADATA)
$(MANAGERLESS_MANIFEST): $(MANAGERLESS_DIR)
	$(KUSTOMIZE) build config/managerless > $@

$(MANAGERLESS_METADATA): semver $(MANAGERLESS_DIR)
	cp $(METADATA_YAML) $@

managerless-cluster-template: semver $(MANAGERLESS_CLUSTER_TEMPLATE)
$(MANAGERLESS_CLUSTER_TEMPLATE): $(MANAGERLESS_DIR)
	cp $(CLUSTER_TEMPLATE) $@

managerless-clusterctl: semver $(MANAGERLESS_CLUSTERCTLYAML)
$(MANAGERLESS_CLUSTERCTLYAML): $(MANAGERLESS_BASE)
	@cat $(CLUSTERCTL_TEMPLATE) | sed 's%URL%$(FULL_MANAGERLESS_MANIFEST)%g' > $@
	@echo "managerless ready, command-line is:"
	@echo "	clusterctl --config=$@ <commands>"

$(COREPATH):
	mkdir -p $@

$(COREPATH)/%:
	curl -s -L -o $@ $(CORE_URL)/$*

core: $(COREPATH)
	# download from core
	@$(eval YAMLS := $(shell curl -s -L $(CORE_API) | jq -r '[.[] | select(.tag_name == "$(CORE_VERSION)").assets[] | select(.name | contains("yaml")) | .name] | join(" ")'))
	@if [ -n "$(YAMLS)" ]; then $(MAKE) $(addprefix $(COREPATH)/,$(YAMLS)); fi

# the standard way to initialize a cluster. If you are using an actually released version,
# then you can just do "clusterctl init --infrastructure=tinkerbell" without any of this
cluster-init: managerless release
	clusterctl init
	clusterctl init --config=$(MANAGERLESS_CLUSTERCTLYAML) --infrastructure=tinkerbell

# this is just for those who really want to see the manual steps
cluster-init-manual: core managerless release
	kubectl apply --validate=false -f $(CERTMANAGER_URL)
	# because of dependencies, this is allowed to fail once or twice
	kubectl apply -f $(COREPATH) || kubectl apply -f $(COREPATH) || kubectl apply -f $(COREPATH)
	kubectl apply -f $(FULL_MANAGERLESS_MANIFEST)

.PHONY: modules
modules: ## Runs go mod to ensure modules are up to date.
	go mod tidy
	cd $(TOOLS_DIR); go mod tidy

