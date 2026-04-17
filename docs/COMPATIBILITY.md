# Compatibility

This document describes the version compatibility between CAPT (Cluster API Provider Tinkerbell) and Cluster API (CAPI).

## Version Matrix

| CAPT Version | CAPI Contract | CAPI Version | API Group Versions | Kubernetes | Go  |
|---|---|---|---|---|---|
| v0.7.x | v1beta2 | >= v1.12.x | `v1beta2` (storage) + `v1beta1` (served) | v1.35+ | 1.25+ |
| v0.5.x - v0.6.x | v1beta1 | v1.6.x - v1.10.x | `infrastructure.cluster.x-k8s.io/v1beta1` | v1.29 - v1.34 | 1.22 - 1.24 |
| v0.3.x - v0.4.x | v1beta1 | v1.3.x - v1.5.x | `infrastructure.cluster.x-k8s.io/v1beta1` | v1.22 - v1.28 | 1.19 - 1.21 |

## API Version Conversion

CAPT v0.7.x introduces `infrastructure.cluster.x-k8s.io/v1beta2` as the new
storage (hub) API version. The previous `v1beta1` version remains served for
backward compatibility. CRD conversion webhooks handle automatic bi-directional
conversion between versions.

- **v1beta2** — The hub (storage) version. New fields like `templateInline`,
  `templateRef`, and `TinkerbellMachineConfig` live here. All resources are
  stored as v1beta2 in etcd.
- **v1beta1** — The spoke (served) version. Existing v1beta1 manifests and
  clients continue to work. Resources are converted on the fly by the webhook.

### Conversion Details

| v1beta1 Field | v1beta2 Field | Notes |
|---|---|---|
| `templateOverride` | `templateInline` | Renamed |
| `templateOverrideRef` | `templateRef` | Renamed, uses `ObjectRef` |
| `status.instanceStatus` | `status.state` | Renamed, same underlying type |
| `imageLookup*` fields | *(removed)* | Dropped during up-conversion; not round-trippable |
| `TinkerbellMachineSpec` (in templates) | `TinkerbellMachineConfig` | `hardwareName`/`providerID` excluded from templates |

The conversion functions are plain exported functions in the `api/v1beta1`
package (e.g. `ConvertClusterToHub`, `ConvertClusterFromHub`). The conversion
webhook registration uses controller-runtime's registry-based mechanism in the
main module, keeping the API module free of controller-runtime dependencies.

## Contract vs API Version

These are independent concepts:

- **Contract version** (`v1beta2`): Defines the behavior and status fields CAPI expects from an infrastructure provider (e.g. `Initialization.Provisioned` status, typed webhook interfaces).
- **API group version** (`v1beta2`): The Kubernetes API version used in CRD definitions and manifests. This is the version used in `apiVersion:` fields of YAML resources.

The CRD labels map contract versions to API versions. For CAPT v0.7.x:

```yaml
# config/crd/kustomization.yaml
labels:
  - pairs:
      cluster.x-k8s.io/v1beta1: v1beta1
      cluster.x-k8s.io/v1beta2: v1beta2  # v1beta2 contract → v1beta2 API version
```

The label **key** is the contract version, the label **value** is the CRD API version that CAPI should use when accessing this provider's resources.

## What Changed in v0.7.x (v1beta2 Contract)

### API Type Changes

| Change | Before (v0.6.x) | After (v0.7.x) |
|---|---|---|
| CAPI import path | `sigs.k8s.io/cluster-api/api/v1beta1` | `sigs.k8s.io/cluster-api/api/core/v1beta2` |
| `ControlPlaneEndpoint` | required, value type | optional, pointer (`*clusterv1.APIEndpoint`) |
| `Machine.Spec.Version` | `*string` | `string` |
| `Cluster.Spec.InfrastructureRef` | `*corev1.ObjectReference` | `clusterv1.ContractVersionedObjectReference` |
| `Cluster.Spec.Paused` | `bool` | `*bool` |
| Webhook interfaces | `webhook.CustomValidator` / `webhook.CustomDefaulter` | `admission.Validator[T]` / `admission.Defaulter[T]` |
| Webhook builder | `ctrl.NewWebhookManagedBy(mgr).For(obj)` | `ctrl.NewWebhookManagedBy(mgr, obj)` |

### New Status Fields (Required by v1beta2 Contract)

Both `TinkerbellCluster` and `TinkerbellMachine` now include `Initialization` status:

```go
// TinkerbellClusterStatus
Initialization *TinkerbellClusterInitializationStatus

// TinkerbellClusterInitializationStatus
Provisioned *bool  // Set to true when infrastructure is ready

// TinkerbellMachineStatus
Initialization *TinkerbellMachineInitializationStatus

// TinkerbellMachineInitializationStatus
Provisioned *bool  // Set to true when machine is provisioned
```

The `Initialization.Provisioned` field is part of the CAPI contract and is used to orchestrate cluster/machine provisioning.

### Controller Changes

- `ClusterPausedTransitionsOrInfrastructureReady` predicate renamed to `ClusterPausedTransitionsOrInfrastructureProvisioned`
- Cluster readiness now checks both `Status.Ready` and `Status.Initialization.Provisioned`

### Manifests and Templates

New manifests should use `v1beta2` API versions:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: TinkerbellCluster
```

Existing `v1beta1` manifests continue to work — the conversion webhook
automatically converts them to `v1beta2` for storage:

```yaml
# Still accepted
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellCluster
```

CAPI v1.12 uses the CRD labels to determine which API version to use. Users
upgrading from v0.6.x do **not** need to update existing manifests, though
new manifests should prefer `v1beta2`.

### metadata.yaml

The `metadata.yaml` file maps release series to contracts for `clusterctl`:

```yaml
releaseSeries:
  - major: 0
    minor: 7
    contract: v1beta2  # New
  - major: 0
    minor: 6
    contract: v1beta1
```

### Dependencies

| Dependency | v0.6.x | v0.7.x |
|---|---|---|
| `sigs.k8s.io/cluster-api` | v1.10.4 | v1.12.5 |
| `sigs.k8s.io/controller-runtime` | v0.21.0 | v0.23.3 |
| `k8s.io/api` | v0.33.x | v0.35.x |
| `tinkerbell/api` | v0.22.x | v0.23.x |
| Go | 1.24.1 | 1.25.0 |
