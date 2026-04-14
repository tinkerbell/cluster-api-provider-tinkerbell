# External Tinkerbell Cluster

By default, CAPT expects Tinkerbell CRDs (Hardware, Template, Workflow, Job) to
live in the same Kubernetes cluster as the CAPI management components. The
**external Tinkerbell cluster** feature lets you point CAPT at a separate cluster
that hosts these objects while CAPI resources remain in the management cluster.

This is useful when the Tinkerbell stack runs on dedicated infrastructure (for
example, co-located with the DHCP/PXE services on a provisioning network) and
the CAPI management cluster is elsewhere.

## Architecture Overview

```
┌──────────────────────────┐        ┌──────────────────────────┐
│   Management Cluster     │        │   Tinkerbell Cluster     │
│                          │        │                          │
│  Cluster API resources   │        │  Hardware                │
│  TinkerbellCluster       │◄──────►│  Template                │
│  TinkerbellMachine       │        │  Workflow                │
│  Machine, Cluster, etc.  │        │  Job (BMC)               │
│                          │        │                          │
│  CAPT controller         │        │  Tinkerbell server       │
└──────────────────────────┘        │  Smee, Tootles, etc.     │
                                    └──────────────────────────┘
```

CAPT uses two separate clients:

- **Management client** — interacts with CAPI objects (`TinkerbellMachine`,
  `TinkerbellCluster`, `Cluster`, `Machine`, etc.) in the management cluster.
- **Tinkerbell client** — interacts with Tinkerbell CRD objects (`Hardware`,
  `Template`, `Workflow`, `Job`) in the external Tinkerbell cluster.

## Objects Watched in the External Cluster

CAPT watches the following object types in the external cluster to react to
changes and reconcile the owning `TinkerbellMachine`:

| Object | API Group | Description |
|---|---|---|
| **Workflow** | `tinkerbell.org/v1alpha1` | Provisioning workflows executed by Tink Worker |
| **Job** | `bmc.tinkerbell.org/v1alpha1` | BMC power-management jobs (Rufio) |

Hardware and Template objects are accessed on demand rather than through the
watch cache.

### Ownership Tracking

Kubernetes owner references do not work across cluster or namespace boundaries.
CAPT uses **labels** on all Tinkerbell resources (Template, Workflow, Job) to
associate them with their owning `TinkerbellMachine`:

| Label | Description |
|---|---|
| `capt.tinkerbell.org/machine-name` | Name of the owning `TinkerbellMachine` |
| `capt.tinkerbell.org/machine-namespace` | Namespace of the owning `TinkerbellMachine` |

These labels are **always set**, regardless of whether CAPT is running in local
or external mode. They enable label-based watches and cleanup for
cross-namespace and cross-cluster resources.

In local mode, when Tinkerbell resources are in the **same namespace** as the
`TinkerbellMachine`, a standard Kubernetes controller owner reference is also
set for backward compatibility. When resources are in a **different namespace**,
only the labels are used because owner references cannot cross namespace
boundaries. See the [QUICK-START guide](QUICK-START.md#cross-namespace-support)
for details on local cross-namespace usage.

## Configuration

Provide credentials for the external Tinkerbell cluster via a **kubeconfig file**
mounted as a Kubernetes Secret. If no kubeconfig is provided, CAPT operates in
local mode (no external cluster).

```bash
# Create the secret in the management cluster
kubectl create secret generic external-tinkerbell-kubeconfig \
  --from-file=kubeconfig=/path/to/external-kubeconfig \
  -n capt-system
```

The default deployment mounts this secret at
`/var/run/secrets/external-tinkerbell/kubeconfig`.

See [REMOTE-TINKERBELL-KUBECONFIG.md](REMOTE-TINKERBELL-KUBECONFIG.md) for a
full walkthrough on creating a ServiceAccount, RBAC rules, and kubeconfig with
the minimum required permissions.

### Controller Flag

| Flag | Default | Description |
|---|---|---|
| `--external-kubeconfig` | `/var/run/secrets/external-tinkerbell/kubeconfig` | Path to a kubeconfig file for the external Tinkerbell cluster |

## JIT (Just-In-Time) Per-Namespace Watches

In external mode, CAPT uses a **direct (non-cached) client** for CRUD operations
and dynamically creates per-namespace informer caches when Hardware is selected
for a machine. When a Hardware object in namespace `foo` is matched, CAPT:

1. Creates a namespace-scoped cache watching Workflows and Jobs in `foo`
2. Starts the cache and waits for it to sync
3. Registers watch sources on the controller for that namespace

Subsequent machines using Hardware in the same namespace reuse the existing
cache (idempotent). This approach does not require the kubeconfig to have
`list` or `watch` access on `Namespace` objects — CAPT discovers namespaces
from the selected Hardware rather than enumerating namespaces. Workflow and
Job watches are namespace-scoped and only need per-namespace RBAC. Hardware,
however, still requires cluster-wide `get`, `list`, and `patch` access via a
ClusterRole because CAPT discovers available Hardware across all namespaces
before a target namespace is known (see
[REMOTE-TINKERBELL-KUBECONFIG.md](REMOTE-TINKERBELL-KUBECONFIG.md) for
details).

## Target Namespace Resolution

All Tinkerbell resources for a machine — Hardware, Template, Workflow, and Job —
are always created in the **same namespace as the selected Hardware object**.

> **Co-location constraint:** `Workflow.Spec.HardwareRef` is a **name-only**
> reference that Tinkerbell resolves within the Workflow's own namespace. All
> resources must be co-located with the Hardware for Tinkerbell to function.

The resolution is simple:

1. Hardware is selected (via `HardwareAffinity` label selectors)
2. The Hardware's namespace becomes the target namespace
3. Template, Workflow, and Job are created in that namespace
4. The namespace is persisted in `TinkerbellMachine.Status.TargetNamespace`
   for consistency across subsequent reconcile loops (including deletion)

This zero-configuration approach requires no namespace flags or spec overrides —
resources are automatically co-located with Hardware.

## Caveats and Limitations

### Orphaned Resources

When Tinkerbell resources live in a different namespace or cluster from the
`TinkerbellMachine`, they use **labels** for ownership tracking instead of
Kubernetes owner references (which do not work across namespace or cluster
boundaries). This applies to both **external mode** and **local
cross-namespace** setups. It means:

- If the management cluster is deleted or becomes unreachable, resources created
  by CAPT on the external Tinkerbell cluster **will not be garbage-collected**
  automatically. They must be cleaned up manually.
- If a `TinkerbellMachine` finalizer is removed without running the controller's
  delete logic, the corresponding Template, Workflow, and Job on the external
  cluster will remain.

To identify orphaned resources, look for the CAPT ownership labels:

```bash
kubectl get templates,workflows,jobs.bmc.tinkerbell.org \
  -l capt.tinkerbell.org/machine-name \
  --all-namespaces
```

To clean up orphaned resources, delete them by label selector. Review the
resources first, then delete:

```bash
# Dry-run: list all CAPT-managed resources
kubectl get templates,workflows,jobs.bmc.tinkerbell.org \
  -l capt.tinkerbell.org/machine-name \
  --all-namespaces -o wide

# Delete orphaned resources (after confirming they are no longer needed)
kubectl delete templates,workflows,jobs.bmc.tinkerbell.org \
  -l capt.tinkerbell.org/machine-name \
  --all-namespaces
```

To clean up resources for a specific machine:

```bash
kubectl delete templates,workflows,jobs.bmc.tinkerbell.org \
  -l capt.tinkerbell.org/machine-name=<machine-name> \
  -l capt.tinkerbell.org/machine-namespace=<machine-namespace> \
  --all-namespaces
```
