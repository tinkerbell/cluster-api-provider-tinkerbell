# Upgrading from CAPT v0.6.8 to v0.7.0

This guide covers the breaking changes, migration steps, and new features when
upgrading Cluster API Provider Tinkerbell (CAPT) from v0.6.8 to v0.7.0.

## Prerequisites

| Requirement | v0.6.8 | v0.7.0 |
|---|---|---|
| Cluster API | v1.6.x – v1.10.x | >= v1.12.5 |
| CAPI Contract | v1beta1 | v1beta2 |
| Kubernetes | v1.29 – v1.34 | v1.35+ |
| kubectl | >= v1.29 | >= v1.33 |
| clusterctl | >= v1.6 | >= v1.12 |
| Go (for building) | 1.22 – 1.24 | 1.25+ |
| Tinkerbell stack | >= v0.18 | >= v0.23 |

> **Note:** CAPT v0.7.0 introduces `infrastructure.cluster.x-k8s.io/v1beta2` as
> the new storage (hub) API version. The previous `v1beta1` version remains
> served for backward compatibility — existing v1beta1 manifests and CRs are
> automatically converted via the CRD conversion webhook. No manual migration
> of existing resources is required. See [COMPATIBILITY.md](COMPATIBILITY.md)
> for details.

## Upgrade Steps

### 1. Update clusterctl configuration

Update your `~/.cluster-api/clusterctl.yaml` to point to the v0.7.0 release:

```yaml
providers:
  - name: "tinkerbell"
    url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/v0.7.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
```

### 2. Upgrade the management cluster

```bash
clusterctl upgrade apply --infrastructure tinkerbell:v0.7.0
```

This upgrades both CAPI core components (to v1.12.5) and the Tinkerbell
infrastructure provider. CRDs are updated automatically.

### 3. Update workload cluster manifests

Apply the breaking changes described below to all `TinkerbellCluster`,
`TinkerbellMachine`, and `TinkerbellMachineTemplate` manifests before
creating new clusters or scaling existing ones.

---

## Breaking Changes

### Default template generation removed

CAPT v0.6.8 could auto-generate a provisioning template using `imageLookup*`
fields. This feature has been removed — users must now provide templates
explicitly.

**Removed fields** (from both `TinkerbellClusterSpec` and `TinkerbellMachineSpec`):

- `spec.imageLookupBaseRegistry`
- `spec.imageLookupOSDistro`
- `spec.imageLookupOSVersion`
- `spec.imageLookupFormat`

**Action required:** If you relied on the default template, you must now provide
a template via `templateInline` or `templateRef`. See
[Customize the provisioning template](QUICK-START.md#customize-the-provisioning-template)
for a full example.

The `TINKERBELL_IP` environment variable is also no longer needed and has been
removed from the controller deployment.

### Field renames

| v0.6.8 | v0.7.0 | Description |
|---|---|---|
| `spec.templateOverride` | `spec.templateInline` | Inline template string |
| `spec.templateOverrideRef` | `spec.templateRef` | Reference to a Tinkerbell Template object |

These renames apply to both `TinkerbellClusterSpec` and `TinkerbellMachineSpec`
(via `TinkerbellMachineConfig`).

**Action required:** Find and replace these field names in all your manifests:

```bash
# Example: update all YAML files in a directory
sed -i 's/templateOverride:/templateInline:/g; s/templateOverrideRef:/templateRef:/g' *.yaml
```

### TinkerbellMachineTemplate spec restructured

The `TinkerbellMachineTemplate` resource's inner spec now uses a new type
`TinkerbellMachineConfig` instead of `TinkerbellMachineSpec`. This prevents
controller-managed fields (`providerID`, `hardwareName`) from appearing in
templates.

**v0.6.8:**
```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: my-template
spec:
  template:
    spec:
      hardwareAffinity: ...
      templateOverride: ...
      # providerID and hardwareName could be set (incorrectly)
```

**v0.7.0:**
```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: my-template
spec:
  template:
    spec:
      hardwareAffinity: ...
      templateInline: ...
      # providerID and hardwareName are structurally impossible here
```

**Action required:** If any `TinkerbellMachineTemplate` sets `providerID` or
`hardwareName`, remove those fields. They are now managed exclusively by the
controller at runtime.

### Mutual exclusivity of templateInline and templateRef

`templateInline` and `templateRef` are now enforced as mutually exclusive via
CEL validation. Setting both on a `TinkerbellCluster` or `TinkerbellMachineTemplate`
will be rejected by the API server.

### Machine finalizer changed

| | Value |
|---|---|
| **v0.6.8** | `tinkerbellmachine.infrastructure.cluster.x-k8s.io` |
| **v0.7.0** | `infrastructure.cluster.x-k8s.io/tinkerbellmachine` |

The controller automatically migrates the old finalizer to the new one during
reconciliation. No manual action is required.

---

## New Features

### Cluster API v1beta2 contract

CAPT v0.7.0 implements the CAPI v1beta2 contract. Key behavioral changes:

- **`Initialization.Provisioned` status** — Both `TinkerbellCluster` and
  `TinkerbellMachine` now report an `Initialization` status block with a
  `Provisioned` boolean. CAPI uses this to orchestrate provisioning.
- **Typed webhook interfaces** — Webhooks use Go generics
  (`admission.Validator[T]` / `admission.Defaulter[T]`) instead of untyped
  `runtime.Object`.
- **Paused condition** — Uses `paused.EnsurePausedCondition` for v1beta2
  compliance.

### Cluster-level template

`TinkerbellClusterSpec` now supports `templateInline` and `templateRef` fields,
providing a cluster-wide default template for all machines. Individual machines
can still override this with their own template.

**Template selection precedence:**

1. `TinkerbellMachineTemplate.spec.template.spec.templateInline` (or `templateRef`)
2. Hardware annotation `hardware.tinkerbell.org/capt-template-override`
3. `TinkerbellCluster.spec.templateInline` (or `templateRef`)

### External Tinkerbell cluster support

CAPT can now manage Tinkerbell resources (Hardware, Template, Workflow, Job) on
a separate Kubernetes cluster from the CAPI management cluster. This is useful
when the Tinkerbell stack runs on dedicated provisioning infrastructure.

See [REMOTE-TINKERBELL.md](REMOTE-TINKERBELL.md) for setup instructions.

### Cross-namespace resource support

CAPI objects and Tinkerbell objects no longer need to be in the same namespace.
CAPT creates Template, Workflow, and Job resources in the Hardware's namespace
and uses label-based ownership tracking:

| Label | Description |
|---|---|
| `capt.tinkerbell.org/machine-name` | Name of the owning `TinkerbellMachine` |
| `capt.tinkerbell.org/machine-namespace` | Namespace of the owning `TinkerbellMachine` |

### Hardware template override annotation

Set the `hardware.tinkerbell.org/capt-template-override` annotation on a
Hardware object to the name of an existing Tinkerbell `Template` in the same
namespace. This enables per-hardware template selection without modifying CAPI
manifests.

### API module extraction

The API types (`github.com/tinkerbell/cluster-api-provider-tinkerbell/api`) are
now published as a separate Go module with minimal dependencies. Downstream
consumers can import the API types without pulling in `controller-runtime`.

---

## Migration Example

### Before (v0.6.8)

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellCluster
metadata:
  name: my-cluster
spec:
  imageLookupBaseRegistry: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell
  imageLookupOSDistro: ubuntu
  imageLookupOSVersion: "2404"
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: my-cluster-control-plane
spec:
  template:
    spec:
      templateOverride: |
        version: "0.1"
        name: my-template
        global_timeout: 6000
        tasks:
          - name: "provision"
            worker: "{{.device_1}}"
            actions:
              - name: "stream image"
                image: quay.io/tinkerbell/actions/oci2disk
                timeout: 1200
                environment:
                  IMG_URL: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu-2404:v1.35.0.gz
                  DEST_DISK: {{ index .Hardware.Disks 0 }}
                  COMPRESSED: true
```

### After (v0.7.0)

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: TinkerbellCluster
metadata:
  name: my-cluster
spec:
  # imageLookup* fields removed
  # Cluster-level template (optional, applies to all machines)
  templateInline: |
    version: "0.1"
    name: my-template
    global_timeout: 6000
    tasks:
      - name: "provision"
        worker: "{{.device_1}}"
        actions:
          - name: "stream image"
            image: quay.io/tinkerbell/actions/oci2disk
            timeout: 1200
            environment:
              IMG_URL: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu-2404:v1.35.0.gz
              DEST_DISK: {{ index .Hardware.Disks 0 }}
              COMPRESSED: true
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: TinkerbellMachineTemplate
metadata:
  name: my-cluster-control-plane
spec:
  template:
    spec: {}
    # Or override the cluster-level template per machine:
    # spec:
    #   templateInline: |
    #     ...
```

---

## Dependency Versions

| Dependency | v0.6.8 | v0.7.0 |
|---|---|---|
| `sigs.k8s.io/cluster-api` | v1.10.4 | v1.12.5 |
| `sigs.k8s.io/controller-runtime` | v0.21.0 | v0.23.3 |
| `k8s.io/api` | v0.33.x | v0.35.3 |
| `k8s.io/apimachinery` | v0.33.x | v0.35.3 |
| `k8s.io/client-go` | v0.33.x | v0.35.3 |
| `github.com/tinkerbell/tinkerbell/api` | v0.22.x | v0.23.0 |

## API Import Path Changes (Go consumers)

If you import CAPT API types in Go code:

```go
// v0.6.8
import infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"

// v0.7.0 — new hub (storage) version
import infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta2"
// go.mod: require github.com/tinkerbell/cluster-api-provider-tinkerbell/api v0.7.0

// The v1beta1 package is still available for backward compatibility.
// Conversion functions are provided to convert between versions:
import v1beta1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
```

The API module (`github.com/tinkerbell/cluster-api-provider-tinkerbell/api`) no
longer depends on `sigs.k8s.io/controller-runtime` directly. Conversion logic
uses plain functions (`ConvertClusterToHub`, `ConvertClusterFromHub`, etc.)
rather than interface methods.
