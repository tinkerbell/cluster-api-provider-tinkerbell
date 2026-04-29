# Quick Start

In this tutorial we’ll cover the basics of how to use Cluster API to create one or more Kubernetes clusters.

## Try it with the Playground

If you want to quickly experiment with CAPT in a local virtual environment, see the
[CAPT Playground](https://github.com/tinkerbell/playground/tree/main/capt). It automates
the entire setup — including a KinD management cluster, Tinkerbell stack, virtual machines,
and a virtual BMC — so you can create a workload cluster with a single command:

```bash
task create-playground
```

The rest of this guide covers manual setup on real (or pre-existing virtual) infrastructure.

## Installation

### Prerequisites

- [kubectl] >= v1.33
- [clusterctl] >= v1.12
- [Helm] >= v3.13
- A running [Tinkerbell stack] >= v0.23.

### Initialize the management cluster

Now that we've got clusterctl installed and all the prerequisites in place, let's transform the Kubernetes cluster
into a management cluster by using `clusterctl init`. This only needs to be run once. For this quick start, run this against the same
Kubernetes cluster where the Tinkerbell stack is installed.

The command accepts as input a list of providers to install; when executed for the first time, `clusterctl init`
automatically adds to the list the `cluster-api` core provider, and if unspecified, it also adds the `kubeadm` bootstrap
and `kubeadm` control-plane providers.

`clusterctl` doesn't include the Tinkerbell CAPI provider (CAPT) so we need to configure this:

```sh
cat >> ~/.cluster-api/clusterctl.yaml <<EOF
providers:
  - name: "tinkerbell"
    url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/v0.7.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# Initialize the management cluster
clusterctl init --infrastructure tinkerbell
```

The output of `clusterctl init` is similar to the following:

```shell
Fetching providers
Installing cert-manager Version="v1.17.2"
Waiting for cert-manager to be available...
Installing Provider="cluster-api" Version="v1.12.5" TargetNamespace="capi-system"
Installing Provider="bootstrap-kubeadm" Version="v1.12.5" TargetNamespace="capi-kubeadm-bootstrap-system"
Installing Provider="control-plane-kubeadm" Version="v1.12.5" TargetNamespace="capi-kubeadm-control-plane-system"
Installing Provider="infrastructure-tinkerbell" Version="v0.7.0" TargetNamespace="capt-system"

Your management cluster has been initialized successfully!

You can now create your first workload cluster by running the following:

  clusterctl generate cluster [name] --kubernetes-version [version] | kubectl apply -f -
```

### Create Hardware resources to make Tinkerbell Hardware available

Cluster API Provider Tinkerbell does not assume all hardware configured in Tinkerbell is available for provisioning.
To make Tinkerbell Hardware available create a `Hardware` resource in the management cluster.
Hardware objects should live in the same namespace as the Tinkerbell stack
(specifically where `tink-controller` lives). CAPT will automatically create
Template and Workflow resources in the Hardware's namespace, even if the CAPI
objects are in a different namespace.

CAPT validates the following fields on each Hardware object during provisioning:

- `spec.interfaces` — at least one interface must be defined
- `spec.interfaces[0].dhcp` — the first interface must have DHCP configured
- `spec.interfaces[0].dhcp.ip.address` — the DHCP configuration must include an IP address
- `spec.disks` — at least one disk must be defined

CAPT also requires the following field (used but not explicitly validated at reconciliation time):

- `spec.metadata.instance.id` — must be set. CAPT maps this value to the `device_1` workflow variable, which identifies the target device in every Workflow. It is also used to construct per-hardware ISO URL paths when using ISO boot modes.

The `spec.metadata.instance.hostname` field is optional but recommended.
The `spec.interfaces[0].netboot` block and `labels` field are also optional. Labels are used
for [hardware affinity](#select-your-hardware) matching.

**NOTE:** If the Hardware has a `spec.bmcRef`, CAPT uses it for boot mode
operations (netboot, ISO, custom) and for powering off the machine during
deletion. If `spec.bmcRef` is **not** set, the `bootOptions.bootMode` setting
on the `TinkerbellMachineTemplate` is silently ignored and no power management
is performed.

An example of a valid Hardware resource:

```yaml
apiVersion: tinkerbell.org/v1alpha1
kind: Hardware
metadata:
  name: node-1
  namespace: tink-system
  labels:
    tinkerbell.org/role: worker
spec:
  disks:
    - device: /dev/sda
  interfaces:
    - dhcp:
        arch: x86_64
        hostname: node-1
        ip:
          address: 192.168.1.10
          gateway: 192.168.1.1
          netmask: 255.255.255.0
        lease_time: 86400
        mac: "aa:bb:cc:dd:ee:ff"
        name_servers:
          - 8.8.8.8
        uefi: true
      netboot:
        allowPXE: true
        allowWorkflow: true
  metadata:
    instance:
      hostname: node-1
      id: "aa:bb:cc:dd:ee:ff"
```

**NOTE:** The `metadata.name` and `spec.interfaces[0].dhcp.mac` must be unique across all Hardware objects.

### Create your first workload cluster

Once the management cluster is ready, you can create your first workload cluster.

#### Preparing the workload cluster configuration

The `clusterctl generate cluster` command returns a YAML template for creating a [workload cluster].

#### Required configuration for the Tinkerbell provider

Depending on the infrastructure provider you are planning to use, some additional prerequisites should be satisfied
before configuring a cluster with Cluster API. Instructions are provided for the Tinkerbell provider below.

Otherwise, you can look at the `clusterctl generate cluster` [command][clusterctl generate cluster] documentation for details about how to
discover the list of variables required by a cluster templates.

To see all required Tinkerbell environment variables execute:

```bash
clusterctl generate cluster --infrastructure tinkerbell --list-variables capi-quickstart
```

```bash
# Set CONTROL_PLANE_VIP to an available IP address for the network
# the machines will be provisioned on
export CONTROL_PLANE_VIP=192.168.1.110

# POD_CIDR is overridden here to avoid conflicting with the assumed
# Machine network of 192.168.1.0/24, this can be omitted if the
# Machine network does not conflict with the default of
# 192.168.0/0/16 or can be set to a different value if needed
export POD_CIDR=172.25.0.0/16

# SERVICE_CIDR can be overridden if the default of 172.26.0.0/16
# would interfere with the Machine network
#export SERVICE_CIDR=10.10.0.0/16
```

#### Generating the cluster configuration

For the purpose of this tutorial, we'll name our cluster capi-quickstart.

The `--target-namespace` specifies where CAPI objects (Cluster, Machine, etc.)
are created. CAPT will automatically create Tinkerbell resources (Template,
Workflow) in the same namespace as the selected Hardware, even if that differs
from the CAPI namespace. For the simplest setup, use the namespace where both
the Tink stack and Hardware objects live.

##### Cross-namespace support

CAPI objects and Tinkerbell objects do not need to be in the same namespace.
For example, you can have CAPI objects in a `capi-cluster` namespace while
Hardware lives in the `tinkerbell` namespace. CAPT will create Template,
Workflow, and Job resources in the Hardware's namespace and use label-based
watches to reconcile them back to the `TinkerbellMachine`.

When resources are in different namespaces, Kubernetes owner references are
not set (they cannot cross namespace boundaries). Cleanup relies on CAPT's
finalizer-driven deletion logic and ownership labels
(`capt.tinkerbell.org/machine-name`, `capt.tinkerbell.org/machine-namespace`).
If the CAPT controller is not running when a `TinkerbellMachine` is
force-deleted, orphaned resources may remain. See
[REMOTE-TINKERBELL.md](REMOTE-TINKERBELL.md#orphaned-resources) for cleanup
instructions.

```bash
clusterctl generate cluster capi-quickstart \
  --kubernetes-version v1.35.2 \
  --control-plane-machine-count=3 \
  --worker-machine-count=3 \
  --target-namespace=tink-system \
  > capi-quickstart.yaml
```

This creates a YAML file named `capi-quickstart.yaml` with a predefined list of Cluster API objects; Cluster, Machines,
Machine Deployments, etc.

The file can be eventually modified using your editor of choice.

See [clusterctl generate cluster] for more details.

#### Select your hardware

In the `capi-quickstart.yaml`, you'll see a `TinkerbellMachineTemplate` type where you can edit the `hardwareAffinity`
to enforce which CAPI machines will get provisioned on particular hardware.

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: capi-quickstart-md-0
  namespace: tink-system
spec:
  template:
    spec:
      hardwareAffinity:
        required:  # 'required' entries are OR'd
        - labelSelector:
            matchLabels:
              manufacturer: dell
            matchExpressions:
            - key: idracVersion
              operator: In
              values: ["7", "8"]
        preferred: # 'preferred' entries are sorted by weight and then sequentially evaluated
        - weight: 50
          hardwareAffinityTerm:
            labelSelector:
              matchLabels:
                rack: 1
                room: 2
```

#### Customize the provisioning template

By default, CAPT generates a Tinkerbell Template for each machine based on the OS image settings in the
`TinkerbellCluster` and `TinkerbellMachineTemplate`. For most deployments you will want to override the
default template to control exactly which actions run during provisioning.

Set the `templateOverride` field in the `TinkerbellMachineTemplate` spec with a full
[Tinkerbell Template](https://docs.tinkerbell.org) definition. The template supports Go template
variables for hardware-specific values:

| Variable | Description |
|---|---|
| `{{.device_1}}` | Worker device identifier (maps to `spec.metadata.instance.id` from the Hardware) |
| `{{ index .Hardware.Disks 0 }}` | First disk device path (e.g. `/dev/sda`) |
| `{{ formatPartition (index .Hardware.Disks 0) N }}` | Nth partition of the first disk |

**IMPORTANT:** Every `task` block in the template **must** include `worker: "{{.device_1}}"`. This
is how Tinkerbell determines which device should execute the task. The value is resolved at
workflow creation time from the Hardware's `spec.metadata.instance.id` via the Workflow `HardwareMap`.

##### Template selection precedence

When CAPT creates a Tinkerbell Template for a machine, it evaluates these sources in order
and uses the first one found:

1. **`TinkerbellMachineTemplate.spec.template.spec.templateOverride`** — machine-level override, set directly in the CAPI manifest.
2. **Hardware annotation `hardware.tinkerbell.org/capt-template-override`** — set the annotation value to the name of an existing `Template` resource in the same namespace as the Hardware. This enables per-hardware template overrides without changing CAPI objects.
3. **`TinkerbellCluster.spec.templateOverride`** (or `templateOverrideRef`) — cluster-wide override applied to all machines that don't match a higher-priority source.

Here is an example that streams an OS image to disk, configures cloud-init for the
Tinkerbell metadata service, and kexecs into the installed OS:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: capi-quickstart-control-plane
  namespace: tink-system
spec:
  template:
    spec:
      bootOptions:
        bootMode: netboot
      hardwareAffinity:
        required:
        - labelSelector:
            matchLabels:
              tinkerbell.org/role: control-plane
      templateOverride: |
        version: "0.1"
        name: my-provisioning-template
        global_timeout: 6000
        tasks:
          - name: "my-provisioning-template"
            worker: "{{.device_1}}"
            volumes:
              - /dev:/dev
            actions:
              - name: "stream image"
                image: quay.io/tinkerbell/actions/oci2disk
                timeout: 1200
                environment:
                  # Replace 'amd64' with the target node architecture (amd64 or arm64)
                  IMG_URL: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu:2404-v1.35.2-amd64.gz
                  DEST_DISK: {{ index .Hardware.Disks 0 }}
                  COMPRESSED: true
              - name: "add tink cloud-init config"
                image: quay.io/tinkerbell/actions/writefile
                timeout: 90
                environment:
                  DEST_DISK: {{ formatPartition ( index .Hardware.Disks 0 ) 3 }}
                  FS_TYPE: ext4
                  DEST_PATH: /etc/cloud/cloud.cfg.d/10_tinkerbell.cfg
                  UID: 0
                  GID: 0
                  MODE: 0600
                  DIRMODE: 0700
                  CONTENTS: |
                    datasource:
                      Ec2:
                        metadata_urls: ["http://<TINKERBELL_VIP>:7080"]
                        strict_id: false
                    system_info:
                      default_user:
                        name: tink
                        groups: [wheel, adm]
                        sudo: ["ALL=(ALL) NOPASSWD:ALL"]
                        shell: /bin/bash
                    manage_etc_hosts: localhost
                    warnings:
                      dsid_missing_source: off
              - name: "add tink cloud-init ds-config"
                image: quay.io/tinkerbell/actions/writefile
                timeout: 90
                environment:
                  DEST_DISK: {{ formatPartition ( index .Hardware.Disks 0 ) 3 }}
                  FS_TYPE: ext4
                  DEST_PATH: /etc/cloud/ds-identify.cfg
                  UID: 0
                  GID: 0
                  MODE: 0600
                  DIRMODE: 0700
                  CONTENTS: |
                    datasource: Ec2
              - name: "kexec image"
                image: ghcr.io/jacobweinstock/waitdaemon:latest
                timeout: 90
                pid: host
                environment:
                  BLOCK_DEVICE: {{ formatPartition ( index .Hardware.Disks 0 ) 1 }}
                  FS_TYPE: vfat
                  IMAGE: quay.io/tinkerbell/actions/kexec
                  WAIT_SECONDS: 5
                volumes:
                  - /var/run/docker.sock:/var/run/docker.sock
```

##### Cloud-init integration

CAPT expects the provisioned OS to use [cloud-init](https://cloud-init.io/) with an
EC2-compatible metadata datasource. The provisioning template must write two configuration
files so that cloud-init discovers the Tinkerbell metadata service:

| File | Purpose |
|---|---|
| `/etc/cloud/cloud.cfg.d/10_tinkerbell.cfg` | Configures the `Ec2` datasource with the Tinkerbell metadata URL (port `7080`) and any default user settings. |
| `/etc/cloud/ds-identify.cfg` | Tells `ds-identify` to use the `Ec2` datasource (`datasource: Ec2`). |

Both files are shown in the example template above. Without them, cloud-init will not
contact the Tinkerbell metadata service and bootstrap data (kubeadm join tokens, etc.)
will not be applied to the machine.

**`PROVIDER_ID` placeholder:** CAPT looks for the literal string `PROVIDER_ID` in the
bootstrap cloud-config (from the CAPI `Machine`'s bootstrap `Secret`) and replaces it with
the actual provider ID (`tinkerbell://<namespace>/<hardware-name>`) before writing the
data to `Hardware.spec.userData`. If you are using the default CAPI Kubeadm bootstrap
provider, this is handled automatically and no action is needed. This is only relevant
if you are using a custom (non-Kubeadm) bootstrap provider that generates its own
cloud-config — in that case, include the literal string `PROVIDER_ID` wherever the
provider ID is needed and CAPT will substitute the real value at provisioning time.
If you are unsure which bootstrap provider you are using, it is almost certainly the
Kubeadm bootstrap provider and you can ignore this.

##### Boot modes

The `bootOptions.bootMode` field controls how the machine boots into the provisioning
environment (HookOS). Available modes:

| Boot Mode | Description |
|---|---|
| `netboot` | PXE/iPXE network boot (default) |
| `isoboot` | Boot from an ISO image served by the Tinkerbell stack |
| `iso` | Deprecated alias for `isoboot`. Use `isoboot` instead. |
| `customboot` | Custom BMC boot actions |

**NOTE:** Boot modes only take effect when the Hardware has `spec.bmcRef` set (see
[Hardware requirements](#create-hardware-resources-to-make-tinkerbell-hardware-available)).
When using `iso` or `isoboot`, the `bootOptions.isoURL` field is required. CAPT inserts
the Hardware's `spec.metadata.instance.id` (with `:` replaced by `-`) into the ISO URL
path to enable per-hardware ISO customization.

##### Provisioned annotation

When provisioning completes, CAPT sets the annotation
`v1alpha1.tinkerbell.org/provisioned: "true"` on the Hardware object. This is how CAPT
determines that the machine is ready. Do not set or remove this annotation manually
unless you are troubleshooting a stuck machine.

For a complete working example including kube-vip, SSH keys, and kustomize overlays,
see the [CAPT Playground templates](https://github.com/tinkerbell/playground/tree/main/capt/templates).

#### Apply the workload cluster

When ready, run the following command to apply the cluster manifest.

```bash
kubectl apply -f capi-quickstart.yaml
```

The output is similar to this:

```bash
cluster.cluster.x-k8s.io/capi-quickstart created
tinkerbellcluster.infrastructure.cluster.x-k8s.io/capi-quickstart created
kubeadmcontrolplane.controlplane.cluster.x-k8s.io/capi-quickstart-control-plane created
tinkerbellmachinetemplate.infrastructure.cluster.x-k8s.io/capi-quickstart-control-plane created
machinedeployment.cluster.x-k8s.io/capi-quickstart-md-0 created
tinkerbellmachinetemplate.infrastructure.cluster.x-k8s.io/capi-quickstart-md-0 created
kubeadmconfigtemplate.bootstrap.cluster.x-k8s.io/capi-quickstart-md-0 created
```

#### Accessing the workload cluster

The cluster will now start provisioning. You can check status with:

```bash
kubectl get cluster
```

You can also get an "at glance" view of the cluster and its resources by running:

```bash
clusterctl describe cluster capi-quickstart
```

To verify the first control plane is up:

```bash
kubectl get kubeadmcontrolplane
```

You should see an output is similar to this:

```bash
NAME                            INITIALIZED   API SERVER AVAILABLE   VERSION   REPLICAS   READY   UPDATED   UNAVAILABLE
capi-quickstart-control-plane   true                                 v1.35.2   3                  3         3
```

**NOTE** The control plane won't be `Ready` until we install a CNI in the next step.

After the first control plane node is up and running, we can retrieve the [workload cluster] Kubeconfig:

```bash
clusterctl get kubeconfig capi-quickstart > capi-quickstart.kubeconfig
```

### Deploy a CNI solution

For the purposes of this quick start we will deploy Cilium as a CNI solution,
if deploying another CNI solution, you may need to make configuration changes
either in the CNI deployment or in the Cluster deployment manifests to ensure
a working cluster.

```bash
helm repo add cilium https://helm.cilium.io/
helm install cilium cilium/cilium --version 1.17.3 \
  --namespace kube-system \
  --kubeconfig=capi-quickstart.kubeconfig
```

### Clean Up

Delete workload cluster.

```bash
kubectl delete cluster capi-quickstart
```

**IMPORTANT:** Always delete the Cluster object (`kubectl delete cluster <name>`) rather than
deleting the full manifest (`kubectl delete -f capi-quickstart.yaml`). CAPI's Cluster controller
orchestrates cascading deletion in the correct order — Machines are drained and deprovisioned
before infrastructure is torn down, and Tinkerbell resources (Templates, Workflows) are cleaned
up by CAPT's finalizers. Deleting with `-f` sends parallel delete requests that can bypass this
ordering and leave Hardware stuck with finalizers or orphaned Workflows.

**NOTE:** The OS images used in this quick start are available in the [Tinkerbell container registry](https://github.com/orgs/tinkerbell/packages?repo_name=cluster-api-provider-tinkerbell).

<!-- links -->
[bootstrap cluster]: https://cluster-api.sigs.k8s.io/reference/glossary.html#bootstrap-cluster
[clusterctl generate cluster]: https://cluster-api.sigs.k8s.io/clusterctl/commands/generate-cluster.html
[clusterctl get kubeconfig]: https://cluster-api.sigs.k8s.io/clusterctl/commands/get-kubeconfig.html
[clusterctl]: https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl
[Docker]: https://docs.docker.com/get-docker/
[infrastructure provider]: https://cluster-api.sigs.k8s.io/reference/glossary.html#infrastructure-provider
[kind]: https://kind.sigs.k8s.io/docs/user/quick-start
[KubeadmControlPlane]: https://cluster-api.sigs.k8s.io/developer/architecture/controllers/control-plane.html
[kubectl]: https://kubernetes.io/docs/tasks/tools/#kubectl
[management cluster]: https://cluster-api.sigs.k8s.io/reference/glossary.html#management-cluster
[provider]: https://cluster-api.sigs.k8s.io/reference/providers.html
[provider components]: https://cluster-api.sigs.k8s.io/reference/glossary.html#provider-components
[workload cluster]: https://cluster-api.sigs.k8s.io/reference/glossary.html#workload-cluster
[Helm]: https://helm.sh/docs/intro/install/
[Tinkerbell]: https://tinkerbell.org
[Tinkerbell stack]: https://github.com/tinkerbell/tinkerbell/tree/main/helm/tinkerbell#readme
