# Copyright 2022 The Tinkerbell Authors.
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

# If you update this file, please follow
# https://www.thapaliya.com/en/writings/well-documented-makefiles/

# Ensure Make is run with bash shell as some syntax below is bash-specific
SHELL:=/usr/bin/env bash

.DEFAULT_GOAL:=help

GOPROXY := $(shell go env GOPROXY)
ifeq ($(GOPROXY),)
GOPROXY := https://proxy.golang.org
endif
export GOPROXY

# Directories.
TOOLS_BIN_DIR := $(abspath bin)

# Binaries.
CONTROLLER_GEN_VER := v0.19.0
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(TOOLS_BIN_DIR)/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER)

GOLANGCI_LINT_VER := v2.3.0
GOLANGCI_LINT_BIN := golangci-lint
GOLANGCI_LINT := $(TOOLS_BIN_DIR)/$(GOLANGCI_LINT_BIN)-$(GOLANGCI_LINT_VER)

KUSTOMIZE_VER := v5.7.1
KUSTOMIZE_BIN := kustomize
KUSTOMIZE := $(TOOLS_BIN_DIR)/$(KUSTOMIZE_BIN)-$(KUSTOMIZE_VER)

GORELEASER_VER := v2.12.2
GORELEASER_BIN := goreleaser
GORELEASER := $(TOOLS_BIN_DIR)/$(GORELEASER_BIN)-$(GORELEASER_VER)

.PHONY: tools
tools: $(KUSTOMIZE) $(GOLANGCI_LINT) $(GORELEASER) $(CONTROLLER_GEN) ## Install build tools

# Define Docker related variables. Releases should modify and double check these vars.
REGISTRY ?= ghcr.io
IMAGE_NAME ?= tinkerbell/cluster-api-provider-tinkerbell

# Allow overriding manifest generation destination directory
MANIFEST_ROOT ?= config
CRD_ROOT ?= $(MANIFEST_ROOT)/crd/bases
WEBHOOK_ROOT ?= $(MANIFEST_ROOT)/webhook
RBAC_ROOT ?= $(MANIFEST_ROOT)/rbac

# Build time versioning details.
LDFLAGS := "-s -w"

## --------------------------------------
## Help
## --------------------------------------

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

## --------------------------------------
## Testing
## --------------------------------------

.PHONY: test
test: ## Run tests
	go test -v ./... -coverprofile cover.out

## --------------------------------------
## Tooling Binaries
## --------------------------------------

$(KUSTOMIZE): ## Install kustomize
	mkdir -p $(TOOLS_BIN_DIR)
	GOBIN=$(TOOLS_BIN_DIR) go install sigs.k8s.io/kustomize/kustomize/v5@${KUSTOMIZE_VER}
	@mv $(TOOLS_BIN_DIR)/kustomize $(KUSTOMIZE)

$(GORELEASER): ## Install goreleaser
	mkdir -p $(TOOLS_BIN_DIR)
	GOBIN=$(TOOLS_BIN_DIR) go install github.com/goreleaser/goreleaser/v2@${GORELEASER_VER}
	@mv $(TOOLS_BIN_DIR)/goreleaser $(GORELEASER)

$(CONTROLLER_GEN): ## Install controller-gen
	mkdir -p $(TOOLS_BIN_DIR)
	GOBIN=$(TOOLS_BIN_DIR) go install sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VER}
	@mv $(TOOLS_BIN_DIR)/controller-gen $(CONTROLLER_GEN)

## --------------------------------------
## Generate
## --------------------------------------

.PHONY: modules
modules: ## Runs go mod to ensure proper vendoring.
	go mod tidy

.PHONY: generate
generate: ## Generate code
	$(MAKE) generate-go
	$(MAKE) generate-manifests

.PHONY: generate-go
generate-go: tools ## Runs Go related generate targets
	$(CONTROLLER_GEN) \
		paths=./api/... \
		object:headerFile=./config/boilerplate.go.txt
	go generate ./...

.PHONY: generate-manifests
generate-manifests: tools ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) \
		paths=./api/... \
		crd:crdVersions=v1 \
		rbac:roleName=manager-role \
		output:crd:dir=$(CRD_ROOT) \
		output:webhook:dir=$(WEBHOOK_ROOT) \
		webhook
	$(CONTROLLER_GEN) \
		paths=./controller/... \
		output:rbac:dir=$(RBAC_ROOT) \
		rbac:roleName=manager-role

## --------------------------------------
## Build
## --------------------------------------

.PHONY: build
build: generate $(GORELEASER) ## Build the CAPT binary
	${GORELEASER} build --snapshot --clean

.PHONY: build-image
build-image: $(GORELEASER) ## Build the container image
	${GORELEASER} release --snapshot --clean --verbose

.PHONY: build-image-push
build-image-push: $(GORELEASER) ## Build and push the container image
	${GORELEASER} release --clean --verbose ${GORELEASER_EXTRA_FLAGS}

## --------------------------------------
## Manifest Image Update
## --------------------------------------

.PHONY: set-manifest-image
set-manifest-image:
	$(info Updating kustomize image patch file for default resource)
	sed -i'' -e 's@image: .*@image: '"${MANIFEST_IMG}:$(MANIFEST_TAG)"'@' ./config/default/manager_image_patch.yaml

## --------------------------------------
## Release
## --------------------------------------

# When running in GitHub Actions for a tag, use the tag name from GITHUB_REF
# Otherwise, fall back to git describe to find the latest tag
ifdef GITHUB_REF
    ifeq ($(findstring refs/tags/,$(GITHUB_REF)),refs/tags/)
        RELEASE_TAG := $(shell echo $(GITHUB_REF) | sed -e 's/refs\/tags\///')
    else
        RELEASE_TAG := $(shell git describe --abbrev=0 2>/dev/null)
    endif
else
    RELEASE_TAG := $(shell git describe --abbrev=0 2>/dev/null)
endif
RELEASE_DIR ?= out/release

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

.PHONY: release
release: clean-release
	@echo "Using RELEASE_TAG: $(RELEASE_TAG)"
	$(MAKE) set-manifest-image MANIFEST_IMG=$(REGISTRY)/$(IMAGE_NAME) MANIFEST_TAG=$(RELEASE_TAG)
	$(MAKE) release-manifests
	$(MAKE) release-metadata
	$(MAKE) release-templates

.PHONY: release-manifests
release-manifests: tools $(RELEASE_DIR) ## Builds the manifests to publish with a release
	$(KUSTOMIZE) build config/default > $(RELEASE_DIR)/infrastructure-components.yaml

.PHONY: release-metadata
release-metadata: $(RELEASE_DIR)
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml

.PHONY: release-templates
release-templates: $(RELEASE_DIR)
	cp templates/cluster-template*.yaml $(RELEASE_DIR)/

release-local: ## Builds the manifests for use in local development
	$(MAKE) release RELEASE_DIR=out/release/infrastructure-tinkerbell/$(RELEASE_TAG)

## --------------------------------------
## Cleanup / Verification
## --------------------------------------

.PHONY: clean
clean: clean-bin clean-release ## Remove all generated files

.PHONY: clean-bin
clean-bin: ## Remove all generated binaries
	rm -rf bin

.PHONY: clean-release
clean-release: ## Remove the release folder
	rm -rf $(RELEASE_DIR)

.PHONY: verify
verify: verify-modules verify-gen

.PHONY: verify-modules
verify-modules: modules
	@if !(git diff --quiet HEAD -- go.sum go.mod); then \
		echo "go module files are out of date"; exit 1; \
	fi

.PHONY: verify-gen
verify-gen: generate
	@if !(git diff --quiet HEAD); then \
		echo "generated files are out of date, run make generate"; exit 1; \
	fi

## --------------------------------------
## Linting
## --------------------------------------

# BEGIN: lint-install
# http://github.com/tinkerbell/lint-install

.PHONY: lint
lint: _lint ## Lint codebase

LINT_ROOT := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

LINTERS :=
FIXERS :=

GOLANGCI_LINT_CONFIG := $(LINT_ROOT)/.golangci.yml
$(GOLANGCI_LINT):
	mkdir -p $(TOOLS_BIN_DIR)
	rm -rf $(TOOLS_BIN_DIR)/golangci-lint-*
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(TOOLS_BIN_DIR) $(GOLANGCI_LINT_VER)
	mv $(TOOLS_BIN_DIR)/golangci-lint $@

LINTERS += golangci-lint-lint
golangci-lint-lint: $(GOLANGCI_LINT)
	find . -name go.mod -execdir sh -c '"$(GOLANGCI_LINT)" run -c "$(GOLANGCI_LINT_CONFIG)"' '{}' '+'

FIXERS += golangci-lint-fix
golangci-lint-fix: $(GOLANGCI_LINT)
	find . -name go.mod -execdir "$(GOLANGCI_LINT)" run -c "$(GOLANGCI_LINT_CONFIG)" --fix \;

.PHONY: _lint $(LINTERS)
_lint: $(LINTERS)

.PHONY: fix $(FIXERS)
fix: $(FIXERS)

# END: lint-install
