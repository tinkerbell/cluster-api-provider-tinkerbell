# External Tinkerbell Kubeconfig

This guide walks through creating a ServiceAccount, RBAC rules, and kubeconfig
for the external Tinkerbell cluster so that CAPT has the permissions it needs.

See [REMOTE-TINKERBELL.md](REMOTE-TINKERBELL.md) for an overview of the feature
and all available configuration flags.

## Required Permissions

CAPT performs the following operations on Tinkerbell CRDs in the external cluster:

| Resource | API Group | Verbs |
|---|---|---|
| `hardware` | `tinkerbell.org` | get, list, patch |
| `templates` | `tinkerbell.org` | get, create, delete |
| `workflows` | `tinkerbell.org` | get, list, watch, create, delete |
| `jobs` | `bmc.tinkerbell.org` | get, list, watch, create |

`watch` is required on Workflow and Job because CAPT runs an informer cache that
reacts to status changes on these objects in real time. Hardware is accessed via
direct API calls (not cached), so `watch` is not needed.

> **Note:** Hardware `list` requires **cluster-wide** access (ClusterRole +
> ClusterRoleBinding) because CAPT discovers available Hardware across all
> namespaces before a target namespace is known. A namespace-scoped Role is
> not sufficient for Hardware — the API server will reject a non-namespaced
> list request with 403 Forbidden rather than filtering to authorized
> namespaces. Templates, Workflows, and Jobs can use either cluster-wide or
> namespace-scoped RBAC since they are created in the Hardware's namespace
> after selection.

## Step-by-Step Setup

All commands below should be run against the **external Tinkerbell cluster**.

### 1. Create a Namespace (optional)

If you plan to restrict CAPT to a single namespace in the external cluster, make
sure it exists:

```bash
kubectl create namespace tinkerbell
```

### 2. Create a ServiceAccount

```bash
kubectl create serviceaccount capt-remote -n tinkerbell
```

### 3. Create RBAC Rules

#### Scoped to a Single Namespace (Role + RoleBinding)

Hardware requires a ClusterRole (see note above), but Template, Workflow, and
Job access can be scoped to one namespace:

```yaml
# ClusterRole for hardware (cluster-wide discovery is required)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: capt-remote-hardware
rules:
- apiGroups: ["tinkerbell.org"]
  resources: ["hardware"]
  verbs: ["get", "list", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: capt-remote-hardware
subjects:
- kind: ServiceAccount
  name: capt-remote
  namespace: tinkerbell
roleRef:
  kind: ClusterRole
  name: capt-remote-hardware
  apiGroup: rbac.authorization.k8s.io
---
# Role for namespace-scoped Tinkerbell resources
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: capt-remote
  namespace: tinkerbell
rules:
- apiGroups: ["tinkerbell.org"]
  resources: ["templates"]
  verbs: ["get", "create", "delete"]
- apiGroups: ["tinkerbell.org"]
  resources: ["workflows"]
  verbs: ["get", "list", "watch", "create", "delete"]
- apiGroups: ["bmc.tinkerbell.org"]
  resources: ["jobs"]
  verbs: ["get", "list", "watch", "create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: capt-remote
  namespace: tinkerbell
subjects:
- kind: ServiceAccount
  name: capt-remote
  namespace: tinkerbell
roleRef:
  kind: Role
  name: capt-remote
  apiGroup: rbac.authorization.k8s.io
```

#### Scoped to Multiple Namespaces (Roles + RoleBindings)

When Hardware exists in several namespaces but you do not want to grant
cluster-wide access for Templates, Workflows, and Jobs, create a Role and
RoleBinding **in each namespace**. Hardware still requires a ClusterRole because
CAPT searches for available Hardware across all namespaces.

```yaml
# ClusterRole for hardware (cluster-wide discovery is required)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: capt-remote-hardware
rules:
- apiGroups: ["tinkerbell.org"]
  resources: ["hardware"]
  verbs: ["get", "list", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: capt-remote-hardware
subjects:
- kind: ServiceAccount
  name: capt-remote
  namespace: tinkerbell
roleRef:
  kind: ClusterRole
  name: capt-remote-hardware
  apiGroup: rbac.authorization.k8s.io
```

Then for **each** namespace that contains Tinkerbell resources (e.g. `site-a`,
`site-b`), create a Role and RoleBinding:

```yaml
# Repeat this block for every target namespace
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: capt-remote
  namespace: site-a          # <-- change per namespace
rules:
- apiGroups: ["tinkerbell.org"]
  resources: ["templates"]
  verbs: ["get", "create", "delete"]
- apiGroups: ["tinkerbell.org"]
  resources: ["workflows"]
  verbs: ["get", "list", "watch", "create", "delete"]
- apiGroups: ["bmc.tinkerbell.org"]
  resources: ["jobs"]
  verbs: ["get", "list", "watch", "create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: capt-remote
  namespace: site-a          # <-- change per namespace
subjects:
- kind: ServiceAccount
  name: capt-remote
  namespace: tinkerbell
roleRef:
  kind: Role
  name: capt-remote
  apiGroup: rbac.authorization.k8s.io
```

You can apply these in a loop if you have many namespaces:

```bash
for NS in site-a site-b site-c; do
  cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: capt-remote
  namespace: ${NS}
rules:
- apiGroups: ["tinkerbell.org"]
  resources: ["templates"]
  verbs: ["get", "create", "delete"]
- apiGroups: ["tinkerbell.org"]
  resources: ["workflows"]
  verbs: ["get", "list", "watch", "create", "delete"]
- apiGroups: ["bmc.tinkerbell.org"]
  resources: ["jobs"]
  verbs: ["get", "list", "watch", "create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: capt-remote
  namespace: ${NS}
subjects:
- kind: ServiceAccount
  name: capt-remote
  namespace: tinkerbell
roleRef:
  kind: Role
  name: capt-remote
  apiGroup: rbac.authorization.k8s.io
EOF
done
```

This gives CAPT access only to the specific namespaces you choose, while
keeping Hardware discovery cluster-wide (required for hardware selection).

#### Cluster-Wide (ClusterRole + ClusterRoleBinding)

Use this variant when CAPT needs to operate across all namespaces:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: capt-remote
rules:
- apiGroups: ["tinkerbell.org"]
  resources: ["hardware"]
  verbs: ["get", "list", "patch"]
- apiGroups: ["tinkerbell.org"]
  resources: ["templates"]
  verbs: ["get", "create", "delete"]
- apiGroups: ["tinkerbell.org"]
  resources: ["workflows"]
  verbs: ["get", "list", "watch", "create", "delete"]
- apiGroups: ["bmc.tinkerbell.org"]
  resources: ["jobs"]
  verbs: ["get", "list", "watch", "create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: capt-remote
subjects:
- kind: ServiceAccount
  name: capt-remote
  namespace: tinkerbell
roleRef:
  kind: ClusterRole
  name: capt-remote
  apiGroup: rbac.authorization.k8s.io
```

Apply whichever variant you chose:

```bash
kubectl apply -f capt-remote-rbac.yaml
```

### 4. Generate a Long-Lived Token

Create a Secret that generates a non-expiring token for the ServiceAccount:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: capt-remote-token
  namespace: tinkerbell
  annotations:
    kubernetes.io/service-account.name: capt-remote
type: kubernetes.io/service-account-token
```

```bash
kubectl apply -f capt-remote-token.yaml
```

Wait a moment, then retrieve the token:

```bash
TOKEN=$(kubectl get secret capt-remote-token -n tinkerbell -o jsonpath='{.data.token}' | base64 -d)
```

### 5. Build the Kubeconfig

Replace `<REMOTE_API_SERVER>` with the API server address of the external
Tinkerbell cluster and `<CA_DATA>` with its CA certificate (base64-encoded).

```bash
# Grab the CA from the external cluster
CA_DATA=$(kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
REMOTE_API_SERVER=$(kubectl config view --raw -o jsonpath='{.clusters[0].cluster.server}')

cat > remote-kubeconfig.yaml <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: ${CA_DATA}
    server: ${REMOTE_API_SERVER}
  name: remote-tinkerbell
contexts:
- context:
    cluster: remote-tinkerbell
    user: capt-remote
    namespace: tinkerbell
  name: remote-tinkerbell
current-context: remote-tinkerbell
users:
- name: capt-remote
  user:
    token: ${TOKEN}
EOF
```

### 6. Create the Secret in the Management Cluster

Switch your kubectl context to the **management cluster** and create the Secret
that the CAPT deployment expects:

```bash
kubectl create secret generic external-tinkerbell-kubeconfig \
  --from-file=kubeconfig=remote-kubeconfig.yaml \
  -n capt-system
```

The default CAPT deployment mounts this Secret at `/var/run/secrets/external-tinkerbell/kubeconfig`.

## Verifying

After deploying CAPT, check the controller logs for confirmation that the external
client was configured:

```bash
kubectl logs -n capt-system deployment/capt-controller-manager | grep -i tinkerbellClientMode
```

You should see:

```json
{"level":"info","v":0,"logger":"setup","tinkerbellClientMode":"external","message":"using external Tinkerbell with JIT per-namespace watches"}
```

If no external configuration is found, the log will instead show:

```json
{"level":"info","v":0,"logger":"setup","tinkerbellClientMode":"local","message":"using local Tinkerbell for CRD operations"}
```
