# Upgrades

This document explains how upgrading a cluster works when CAPT is the infrastructure provider:
which component does what, why the process looks the way it does, and what that means on bare
metal.

## Who Does What

An upgrade is shared among three components, and it helps to keep their responsibilities apart.

- **Cluster API** owns the desired state of the cluster. The `KubeadmControlPlane` and
  `MachineDeployment` controllers decide *when* Machines need to change and *which* Machines to
  replace, in what order, and how many at a time.
- **CAPT** turns each new Machine into a physical server. It selects a `Hardware`, builds a
  Tinkerbell `Template` describing how to install an operating system on it, and starts a
  `Workflow` to run that Template.
- **Tinkerbell** network or ISO boots the server and runs the Workflow, which typically writes an OS
  image to disk and boots into it. `kubeadm` then joins the new node to the cluster.

CAPT never decides that an upgrade should happen. It responds to new Machines and deleted
Machines, the same as when a cluster is first created, scaled up, or scaled down.

## Machines Are Replaced, Not Changed

Cluster API treats Machines as immutable. When a Machine's desired spec changes, such as its
Kubernetes version, Cluster API doesn't modify the existing Machine. It creates a new Machine
with the new spec and deletes the old one, a few Machines at a time as the rollout strategy
allows. This is a *rolling update*, following the same model as a Kubernetes `Deployment` rolling
out new Pods.

The reason is predictability. Rebuilding a server from a known image produces the same result
every time, while changing a running server depends on its current state, which may have drifted.
Each node in the cluster is always the result of a single, known provisioning run.

Cluster API also supports *in-place updates*, where an external update extension applies changes
to an existing Machine without replacing it. CAPT does not provide such an extension, so with CAPT
every upgrade replaces Machines.

On bare metal, replacing a Machine means reprovisioning a physical server: network or ISO booting it,
writing a new OS image to its disk, and rebooting or running kexec. This has some implications that are less
visible with virtual machines:

- **Upgrades take time.** Each replacement waits for a full provisioning run, and Machines are
  replaced one or a few at a time, so the total time grows with the number of nodes.
- **Local data doesn't survive.** Generally, provisioning writes a fresh OS image to disk. Unless a custom
  Template preserves other partitions, anything stored only on a node's local disk is lost when
  that node is replaced, even if the replacement lands on the same server.
- **A new Machine may land on a different server.** CAPT selects any available `Hardware` that
  matches the Machine's `hardwareAffinity`. The new Machine isn't tied to the server its
  predecessor ran on.

## What Starts a Rollout

Because Cluster API decides when to replace Machines, only changes to Cluster API objects start
a rollout. The `KubeadmControlPlane` and `MachineDeployment` controllers compare each Machine
with the spec it should have, and replace the ones that differ. The changes that matter for an
upgrade are:

- the Kubernetes `version` on a `KubeadmControlPlane` or `MachineDeployment`, and
- the `infrastructureRef` that points to a `TinkerbellMachineTemplate`.

Cluster API can also be asked to replace every Machine without any spec change, through
`spec.rollout.after` or `clusterctl alpha rollout restart`.

Changes to CAPT objects don't start a rollout. A `TinkerbellCluster`, a Tinkerbell `Template`, or
a Hardware annotation is consulted only when CAPT provisions a new Machine. CAPT builds a
Machine's provisioning Template once, when the Machine is created, and doesn't update it
afterwards. A change to any of these objects is therefore picked up by the next Machine CAPT
provisions, whether that Machine comes from an upgrade, a scale-up, or a failed Machine being
remediated, and has no effect on Machines that already exist.

`TinkerbellMachineTemplate` behaves differently because Cluster API references it directly.
Its spec cannot be changed, which is a Cluster API convention for infrastructure templates. To
change it, you create a new `TinkerbellMachineTemplate` and point `infrastructureRef` at it,
which is itself a change that starts a rollout.

## Where the New Image Comes From

A node's `kubelet` and `kubeadm` come from its OS image, so each Kubernetes version needs its own
image. How the new image is chosen depends on where the provisioning Template comes from; see
[Template selection precedence](QUICK-START.md#template-selection-precedence).

A Template override gives full control over how a server is provisioned, but that control has a
cost during upgrades. Tinkerbell renders an override with details of the `Hardware` only. The
Kubernetes version isn't available to it, so the image URL in an override is fixed text. Every
upgrade requires updating the override, and the update has to be made before the rollout
begins. If the version changes first, new Machines can be provisioned with the old image but
bootstrapped for the new version, and fail to join the cluster.

For the same reason, a `Template` that is referenced by `templateOverrideRef` or by a Hardware
annotation must exist whenever CAPT provisions a Machine. Deleting and recreating it during a
rollout leaves a window in which new Machines can't be provisioned.

## Spare Hardware and Rollout Strategy

By default, Cluster API creates the new Machine before deleting the old one (`maxSurge: 1`), so
the cluster never has fewer nodes than requested. With virtual machines the extra node is created
on demand. With bare metal it has to be an existing, unused server: at least one `Hardware` that
matches the Machine's `hardwareAffinity` and isn't already in use. If there is no such server,
the new Machine waits for Hardware and the rollout doesn't progress.

The alternative is to delete first (`maxSurge: 0`). When an old Machine is deleted, CAPT releases
its Hardware, which makes that server available to the replacement Machine. This avoids the need
for spare servers at the cost of temporarily running with one node fewer. For worker
`MachineDeployment`s that means less capacity for workloads during the rollout, controlled by
`maxUnavailable`. For a `KubeadmControlPlane` it means temporarily running with fewer etcd
members, so Cluster API only allows it with three or more control plane replicas, enough to keep
quorum while one member is missing.

Which approach is better depends on the environment. A spare server per hardware class keeps
full capacity during upgrades and also covers hardware failures, but it is a server that sits idle
most of the time. Deleting first makes use of every server, but each upgrade briefly reduces
capacity and redundancy.

## Upgrade Order and Version Skew

Cluster API upgrades the control plane and the workers separately, and the order matters because
of the Kubernetes [version skew policy](https://kubernetes.io/releases/version-skew-policy/). A
`kubelet` may be older than the API server it talks to, but never newer. Upgrading the control
plane first keeps every node within the supported skew throughout the rollout.

The same policy is why minor versions can't be skipped. `kubeadm` supports upgrading by one minor
version at a time, so moving from v1.35 to v1.37 means passing through v1.36. Each step is a full
rollout, so on bare metal each step reprovisions every server in the cluster.

## Examples

The following examples follow a cluster with three control plane and three worker Machines through
an upgrade from v1.35.2 to v1.36.0. The upgrade has two parts: first, making sure that new
Machines will be provisioned with a v1.36.0 image, and then changing Cluster API objects to start the
rollout.

> [!NOTE]
> These are examples only. Tinkerbell Templates are highly customizable, and the exact changes needed may vary depending on your environment.
> Production environments need careful planning and consideration of their specific requirements.

### Tinkerbell Template Changes

What needs to change depends on where the provisioning Template comes from, as described in
[Where the New Image Comes From](#where-the-new-image-comes-from).

#### Cluster or Hardware Template Override

A `Template` referenced by `TinkerbellCluster` `templateOverrideRef` or by the
`hardware.tinkerbell.org/capt-template-override` Hardware annotation is updated by changing the
image URL in its `spec.data`. The Template keeps its name, so the references to it keep working:

```yaml
apiVersion: tinkerbell.org/v1alpha1
kind: Template
metadata:
  name: ubuntu-2404
  namespace: tinkerbell
spec:
  data: |
    name: ubuntu 2404
    global_timeout: 6000
    tasks:
      - name: "provision"
        worker: "{{.device_1}}"
        volumes:
          - /dev:/dev
        actions:
          - name: "stream image"
            image: quay.io/tinkerbell/actions/oci2disk
            timeout: 1200
            environment:
              # changed from ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu:2404-v1.35.2-amd64.gz
              IMG_URL: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu:2404-v1.36.0-amd64.gz
              DEST_DISK: {{ index .Hardware.Disks 0 }}
              COMPRESSED: true
          # remaining actions unchanged
```

A Template set inline in `TinkerbellCluster` `spec.templateOverride` is updated the same way, by
changing the image URL in that field.

#### Machine Template Override

A `TinkerbellMachineTemplate` cannot be changed, so a new one is created alongside the old one,
here named `mycluster-control-plane-v1-36-0`, with the new image URL in its `templateOverride`:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: mycluster-control-plane-v1-36-0
  namespace: tink-system
spec:
  template:
    spec:
      hardwareAffinity:
        required:
        - labelSelector:
            matchLabels:
              tinkerbell.org/role: control-plane
      templateOverride: |
        # same as the previous TinkerbellMachineTemplate, with IMG_URL changed to
        # ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu:2404-v1.36.0-amd64.gz
```

The new `TinkerbellMachineTemplate` has no effect until Cluster API is pointed at it, as shown in
the next section. The old one is still used by the existing Machines and can be deleted once the
rollout has finished. Workers get their own new `TinkerbellMachineTemplate` in the same way.

### Cluster API Changes

These examples assume that the [Tinkerbell Template changes](#tinkerbell-template-changes) have
already been made, so every new Machine is provisioned with the v1.36.0 image.

If a new `TinkerbellMachineTemplate` was created, the `infrastructureRef` is changed together with
the version, so that both changes are rolled out at once instead of in two separate rollouts:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KubeadmControlPlane
spec:
  version: v1.36.0
  machineTemplate:
    spec:
      infrastructureRef:
        apiGroup: infrastructure.cluster.x-k8s.io
        kind: TinkerbellMachineTemplate
        name: mycluster-control-plane-v1-36-0
```

A `MachineDeployment` has the same field at `spec.template.spec.infrastructureRef`.

The following two examples show how the rollout proceeds with each of the strategies described in
[Spare Hardware and Rollout Strategy](#spare-hardware-and-rollout-strategy).

#### With a Spare Server

In this mode a spare server is available for both the control plane and worker nodes, allowing new Machines to be created on the spare hardware before old ones are deleted.

There are eight `Hardware` objects: `cp-1` through `cp-4` match the control plane's
`hardwareAffinity`, and `worker-1` through `worker-4` match the workers'. One server of each kind,
`cp-4` and `worker-4`, is unused.

The rollout settings are the Cluster API defaults, shown here explicitly:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KubeadmControlPlane
spec:
  replicas: 3
  version: v1.36.0 # changed from v1.35.2, which starts the rollout
  rollout:
    strategy:
      rollingUpdate:
        maxSurge: 1
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
spec:
  replicas: 3
  template:
    spec:
      version: v1.36.0 # changed after the control plane rollout has finished
  rollout:
    strategy:
      type: RollingUpdate
      rollingUpdate:
        maxSurge: 1
        maxUnavailable: 0
```

When the `KubeadmControlPlane` version changes:

1. Cluster API creates a fourth control plane Machine at v1.36.0. CAPT selects `cp-4`, the only
   unused matching server, and provisions it. It joins as a fourth etcd member.
2. Cluster API deletes one of the old Machines, for example the one on `cp-1`. CAPT releases
   `cp-1`, which becomes the unused server.
3. Cluster API creates the next new Machine. CAPT selects `cp-1` and reprovisions it with the
   v1.36.0 image.
4. The cycle repeats until all three control plane Machines run v1.36.0.

The cluster has three or four control plane nodes throughout. The `MachineDeployment` follows
the same pattern with `worker-4`. At the end of the upgrade a different server is spare, here
the last one released, such as `cp-3` and `worker-3`. Spare capacity is a role that moves between
servers, not a fixed server.

#### Without a Spare Server

In this mode no spare server is available, so old Machines must be deleted before new ones are created.

There are only six `Hardware` objects: `cp-1` through `cp-3` and `worker-1` through `worker-3`,
all in use. The rollout settings are changed to delete first:

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: KubeadmControlPlane
spec:
  replicas: 3 # maxSurge: 0 requires at least 3
  version: v1.36.0
  rollout:
    strategy:
      rollingUpdate:
        maxSurge: 0
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: MachineDeployment
spec:
  replicas: 3
  template:
    spec:
      version: v1.36.0
  rollout:
    strategy:
      type: RollingUpdate
      rollingUpdate:
        maxSurge: 0
        maxUnavailable: 1
```

When the `KubeadmControlPlane` version changes:

1. Cluster API deletes one of the old Machines, for example the one on `cp-1`. etcd runs with two
   of its three members, which is still a quorum. CAPT releases `cp-1`.
2. Cluster API creates a new Machine at v1.36.0. CAPT selects `cp-1`, now the only unused
   matching server, and reprovisions it with the v1.36.0 image.
3. Once the new Machine has joined, the cycle repeats for `cp-2` and then `cp-3`.

The cluster has two or three control plane nodes throughout. Every server is reprovisioned in
turn and ends up back in the cluster. The `MachineDeployment` follows the same pattern, running
with two workers while each one is replaced. Workloads on the deleted worker are evicted and must
fit on the remaining two. Raising `maxUnavailable` replaces more workers at once, which finishes
sooner but removes more capacity during the rollout.

With the `v1beta1` APIs, the rollout settings are `spec.rolloutStrategy.rollingUpdate.maxSurge` on
a `KubeadmControlPlane` and `spec.strategy.rollingUpdate` on a `MachineDeployment`. The
`KubeadmControlPlane` `infrastructureRef` is at `spec.machineTemplate.infrastructureRef` and uses
`apiVersion` instead of `apiGroup`.

## Further Reading

- [Upgrading management and workload clusters](https://cluster-api.sigs.k8s.io/tasks/upgrading-clusters):
  the Cluster API book's guide to upgrading clusters, which applies to every infrastructure
  provider, including CAPT.
