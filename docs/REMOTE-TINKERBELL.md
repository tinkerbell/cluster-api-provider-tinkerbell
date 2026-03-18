# Remote Tinkerbell Cluster

By default, CAPT expects Tinkerbell CRDs (Hardware, Template, Workflow, Job) to
live in the same Kubernetes cluster as the CAPI management components. The
**remote Tinkerbell cluster** feature lets you point CAPT at a separate cluster
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
  `Template`, `Workflow`, `Job`) in the remote Tinkerbell cluster.

## Objects Watched in the Remote Cluster

When the remote Tinkerbell cluster feature is enabled, CAPT sets up an informer
cache on the remote cluster that watches the following object types. This allows CAPT to react to changes in these objects and reconcile the owning `TinkerbellMachine` accordingly.

| Object | API Group | Description |
|---|---|---|
| **Workflow** | `tinkerbell.org/v1alpha1` | Provisioning workflows executed by Tink Worker |
| **Job** | `bmc.tinkerbell.org/v1alpha1` | BMC power-management jobs (Rufio) |

Hardware and Template objects are also read from and written to the remote
cluster, but they are accessed on demand rather than through the watch cache.

### Cross-Cluster Ownership

Kubernetes owner references do not work across cluster boundaries. When
operating in remote mode, CAPT uses **labels** instead of owner references to
associate Tinkerbell resources with their owning `TinkerbellMachine`:

| Label | Description |
|---|---|
| `capt.tinkerbell.org/machine-name` | Name of the owning `TinkerbellMachine` |
| `capt.tinkerbell.org/machine-namespace` | Namespace of the owning `TinkerbellMachine` |

These labels are set on `Template`, `Workflow`, and `Job` objects created in the
remote cluster. The informer cache uses them to
[map events back to the correct `TinkerbellMachine` reconcile request](../controller/machine/tinkerbellmachine.go#L322)
via the [`remoteLabelMapper`](../controller/machine/tinkerbellmachine.go#L322) function,
which is [wired into the controller watches](../controller/machine/tinkerbellmachine.go#L208-L217)
in `SetupWithManager`.

When operating in local (same-cluster) mode, standard Kubernetes owner
references are used as before.

## Configuration

Provide credentials for the remote Tinkerbell cluster via a **kubeconfig file**
mounted as a Kubernetes Secret. If no kubeconfig is provided, CAPT operates in
local mode (no remote cluster).

Mount a kubeconfig as a Kubernetes Secret and provide the path to the controller.
See [REMOTE-TINKERBELL-KUBECONFIG.md](REMOTE-TINKERBELL-KUBECONFIG.md) for a
full walkthrough on creating a ServiceAccount, RBAC rules, and kubeconfig with
the minimum required permissions.

```bash
# Create the secret in the management cluster
kubectl create secret generic remote-tinkerbell-kubeconfig \
  --from-file=value=/path/to/remote-kubeconfig \
  -n capt-system
```

The default deployment mounts this secret at `/etc/remote-tinkerbell/value`.

### Watch Namespace

By default the remote informer cache watches all namespaces. To restrict it to a
single namespace:

```
--remote-tinkerbell-watch-namespace=tinkerbell
```

## Controller Flags

| Flag | Default | Description |
|---|---|---|
| `--remote-tinkerbell-kubeconfig` | `/etc/remote-tinkerbell/value` | Path to a kubeconfig file for the remote Tinkerbell cluster |
| `--remote-tinkerbell-watch-namespace` | (empty) | Namespace to watch in the remote cluster; empty means all namespaces |

## Deployment

The default manager deployment in `config/manager/manager.yaml` already includes
the volume mount for the kubeconfig secret and environment variable placeholders
for`clusterctl`. The following variables are substituted at deploy time:

| Variable | Description |
|---|---|
| `REMOTE_TINKERBELL_WATCH_NAMESPACE` | Watch namespace |
| `REMOTE_TINKERBELL_KUBECONFIG` | Name of the kubeconfig Secret (default: `remote-tinkerbell-kubeconfig`) |

### Example: clusterctl

```bash
export REMOTE_TINKERBELL_WATCH_NAMESPACE=tinkerbell

clusterctl init --infrastructure tinkerbell
```

### Example: Tilt (Development)

Set the environment variables before running `tilt up`:

```bash
export REMOTE_TINKERBELL_KUBECONFIG=remote-tinkerbell-kubeconfig

tilt up
```

The Tiltfile performs the variable substitution automatically.
