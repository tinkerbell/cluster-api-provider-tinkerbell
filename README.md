# Cluster API Provider Tinkerbell

[![Go Reference](https://pkg.go.dev/badge/github.com/tinkerbell/cluster-api-provider-tinkerbell.svg)](https://pkg.go.dev/github.com/tinkerbell/cluster-api-provider-tinkerbell)
[![CRD - reference](https://img.shields.io/badge/CRD-reference-2ea44f)](https://doc.crds.dev/github.com/tinkerbell/cluster-api-provider-tinkerbell)
[![Go Report Card](https://goreportcard.com/badge/github.com/tinkerbell/cluster-api-provider-tinkerbell)](https://goreportcard.com/report/github.com/tinkerbell/cluster-api-provider-tinkerbell)

<a href="https://kubernetes.io"><img src="https://github.com/kubernetes/kubernetes/raw/master/logo/logo.png"  height="100"></a>
<a href="https://tinkerbell.org"><img src="https://raw.githubusercontent.com/tinkerbell/artwork/main/Tinkerbell-Logo-Landscape-Dark.png" height="100"></a>

Kubernetes-native declarative infrastructure for Kubernetes clusters using Tinkerbell.

## What is the Cluster API Provider Tinkerbell

The [Cluster API](https://cluster-api.sigs.k8s.io) brings declarative, Kubernetes-style APIs to Kubernetes
cluster creation, configuration and management.

The API itself is shared across multiple cloud providers allowing for true hybrid
deployments of Kubernetes, both on-premises and off.

## Quick Start

See the [Quick Start](docs/QUICK-START.md).

## Kubernetes versions with published Images

Pre-built images are pushed to the [GitHub Container Registry](https://github.com/orgs/tinkerbell/packages?repo_name=cluster-api-provider-tinkerbell). We currently publish images for [Ubuntu 18.04](https://github.com/tinkerbell/cluster-api-provider-tinkerbell/pkgs/container/cluster-api-provider-tinkerbell%2Fubuntu-1804) and [Ubuntu 20.04](https://github.com/tinkerbell/cluster-api-provider-tinkerbell/pkgs/container/cluster-api-provider-tinkerbell%2Fubuntu-2004).

## Current state

See the [release docs](https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases) for each version to find details on features and CAPI compatibility.

See [docs/README.md](docs/README.md) for more information on setting up a development
environment.
