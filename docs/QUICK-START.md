# Quick Start

In this tutorial we’ll cover the basics of how to use Cluster API to create one or more Kubernetes clusters.

## Installation

### Prerequisites

- Install and setup [kubectl] in your local environment
- Install [Kind] and [Docker]
- An existing [Tinkerbell] installation running at least versions mentioned in v0.6.0 of [sandbox](https://github.com/tinkerbell/sandbox/tree/v0.6.0), this guide assumes deployment using the sandbox with an IP address of 192.168.1.1, so modifications will be needed if Tinkerbell was deployed with a different method or if the IP address is different.
- One or more [Hardware](https://docs.tinkerbell.org/hardware-data/) resources defined in Tinkerbell with a DHCP IP address configured on first interface and with proper metadata configured
  - Required metadata for hardware:
    - metadata.facility.facility_code is set (default is "onprem")
    - metadata.instance.id is set (should match the hardware's id)
    - metadata.instance.hostname is set
    - metadata.instance.storage.disks is set and contains at least one device matching an available disk on the system
  - An example of a valid hardware definition for use with the Tinkerbell provider:
    ```json
    {
      "id": "3f0c4d3d-00ef-4e46-983d-0e6b38da827a",
      "metadata": {
        "facility": {
          "facility_code": "onprem"
        },
        "instance": {
          "id": "3f0c4d3d-00ef-4e46-983d-0e6b38da827a",
          "hostname": "hw-a",
          "network": {
            "addresses": [
              {
                "address_family": 4,
                "public": false,
                "address": "192.168.1.105"
              }
            ]
          },
          "storage": {
            "disks": [{"device": "/dev/sda"}]
          }
        },
        "state": ""
      },
      "network": {
        "interfaces": [
          {
            "dhcp": {
              "arch": "x86_64",
              "hostname": "hw-a",
              "ip": {
                "address": "192.168.1.105",
                "gateway": "192.168.1.254",
                "netmask": "255.255.255.0"
              },
              "mac": "a8:a1:59:66:42:89",
      "name_servers": ["8.8.8.8"],
              "uefi": true
            },
            "netboot": {
              "allow_pxe": true,
              "allow_workflow": true
            }
          }
        ]
      }
    }
    ```

### Install and/or configure a Kubernetes cluster

Cluster API requires an existing Kubernetes cluster accessible via kubectl. During the installation process the
Kubernetes cluster will be transformed into a [management cluster] by installing the Cluster API [provider components], so it
is recommended to keep it separated from any application workload.

It is a common practice to create a temporary, local bootstrap cluster which is then used to provision
a target [management cluster] on the selected [infrastructure provider].

Choose one of the options below:

1. **Existing Management Cluster**

   For production use-cases a "real" Kubernetes cluster should be used with appropriate backup and DR policies and procedures in place. The Kubernetes cluster must be at least v1.19.1.

   ```bash
   export KUBECONFIG=<...>
   ```

2. **Kind**

   **NOTE** [kind] is not designed for production use.

   **Minimum [kind] supported version**: v0.9.0

   [kind] can be used for creating a local Kubernetes cluster for development environments or for
   the creation of a temporary [bootstrap cluster] used to provision a target [management cluster] on the selected infrastructure provider.

   Create the kind cluster:
   ```bash
   kind create cluster
   ```
   Test to ensure the local kind cluster is ready:
   ```
   kubectl cluster-info
   ```

### Install clusterctl

The clusterctl CLI tool handles the lifecycle of a Cluster API management cluster.

#### Install clusterctl binary with curl on linux

Download the latest release; on linux, type:
```
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.0.0/clusterctl-linux-amd64 -o clusterctl
```
Make the clusterctl binary executable.
```
chmod +x ./clusterctl
```
Move the binary in to your PATH.
```
sudo mv ./clusterctl /usr/local/bin/clusterctl
```
Test to ensure the version you installed is up-to-date:
```
clusterctl version
```

#### Install clusterctl binary with curl on macOS

Download the latest release; on macOS, type:
```
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.0.0/clusterctl-darwin-amd64 -o clusterctl
```

Or if your Mac has an M1 CPU ("Apple Silicon"):
```
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.0.0/clusterctl-darwin-arm64 -o clusterctl
```

Make the clusterctl binary executable.
```
chmod +x ./clusterctl
```
Move the binary in to your PATH.
```
sudo mv ./clusterctl /usr/local/bin/clusterctl
```
Test to ensure the version you installed is up-to-date:
```
clusterctl version
```

#### Install clusterctl with homebrew on macOS and linux

Install the latest release using homebrew:

```bash
brew install clusterctl
```

Test to ensure the version you installed is up-to-date:
```
clusterctl version
```

### Initialize the management cluster

Now that we've got clusterctl installed and all the prerequisites in place, let's transform the Kubernetes cluster
into a management cluster by using `clusterctl init`.

The command accepts as input a list of providers to install; when executed for the first time, `clusterctl init`
automatically adds to the list the `cluster-api` core provider, and if unspecified, it also adds the `kubeadm` bootstrap
and `kubeadm` control-plane providers.

#### Initialization for the Tinkerbell provider

```sh
# Let clusterctl know about the Tinkerbell provider
cat >> ~/.cluster-api/clusterctl.yaml <<EOF
providers:
  - name: "tinkerbell"
    url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/latest/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# If Tinkerbell is not configured to listen on 192.168.1.1 or
# is configured for different GRPC/HTTP ports, make the appropriate
# changes below
export TINKERBELL_IP=192.168.1.1
export TINKERBELL_GRPC_AUTHORITY=${TINKERBELL_IP}:42113
export TINKERBELL_CERT_URL=http://${TINKERBELL_IP}:42114/cert

# Finally, initialize the management cluster
clusterctl init --infrastructure tinkerbell
```

The output of `clusterctl init` is similar to this:

```shell
Fetching providers
Installing cert-manager Version="v1.5.3"
Waiting for cert-manager to be available...
Installing Provider="cluster-api" Version="v1.0.0" TargetNamespace="capi-system"
Installing Provider="bootstrap-kubeadm" Version="v1.0.0" TargetNamespace="capi-kubeadm-bootstrap-system"
Installing Provider="control-plane-kubeadm" Version="v1.0.0" TargetNamespace="capi-kubeadm-control-plane-system"
Installing Provider="infrastructure-tinkerbell" Version="v0.1.0" TargetNamespace="capt-system"

Your management cluster has been initialized successfully!

You can now create your first workload cluster by running the following:

  clusterctl generate cluster [name] --kubernetes-version [version] | kubectl apply -f -
```

### Create Hardware resources to make Tinkerbell Hardware available

Cluster API Provider Tinkerbell does not assume all hardware configured in Tinkerbell is available for provisioning, to make Tinkerbell Hardware available create a `Hardware` resource in the
management cluster.

An example of a valid Hardware resource definition:
```yaml
---
kind: Hardware
apiVersion: tinkerbell.org/v1alpha1
metadata:
  name: hw-a
spec:
  id: 3f0c4d3d-00ef-4e46-983d-0e6b38da827a
```

**NOTE:** The id should match the id of the Tinkerbell Hardware resource and the name will need to be unique.

### Create your first workload cluster

Once the management cluster is ready, you can create your first workload cluster.

#### Preparing the workload cluster configuration

The `clusterctl generate cluster` command returns a YAML template for creating a [workload cluster].

#### Required configuration for the Tinkerbell provider

Depending on the infrastructure provider you are planning to use, some additional prerequisites should be satisfied
before configuring a cluster with Cluster API. Instructions are provided for the Tinkerbell provider below.

Otherwise, you can look at the `clusterctl generate cluster` [command][clusterctl generate cluster] documentation for details about how to
discover the list of variables required by a cluster templates.

To see all required OpenStack environment variables execute:
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

```bash
clusterctl generate cluster capi-quickstart \
  --kubernetes-version v1.21.6 \
  --control-plane-machine-count=3 \
  --worker-machine-count=3 \
  > capi-quickstart.yaml
```

This creates a YAML file named `capi-quickstart.yaml` with a predefined list of Cluster API objects; Cluster, Machines,
Machine Deployments, etc.

The file can be eventually modified using your editor of choice.

See [clusterctl generate cluster] for more details.

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
capi-quickstart-control-plane   true                                 v1.22.0   3                  3         3
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
kubectl --kubeconfig=capi-quickstart.kubeconfig create -f https://raw.githubusercontent.com/cilium/cilium/v1.9/install/kubernetes/quick-install.yaml
```

### Clean Up

Delete workload cluster.
```bash
kubectl delete cluster capi-quickstart
```

**NOTE** IMPORTANT: In order to ensure a proper cleanup of your infrastructure you must always delete the cluster object. Deleting the entire cluster template with `kubectl delete -f capi-quickstart.yaml` might lead to pending resources to be cleaned up manually.

Delete management cluster
```bash
kind delete cluster
```

## Next steps

See the [clusterctl] documentation for more detail about clusterctl supported actions.

<!-- links -->
[bootstrap cluster]: https://cluster-api.sigs.k8s.io/reference/glossary.html#bootstrap-cluster
[clusterctl generate cluster]: https://cluster-api.sigs.k8s.io/clusterctl/commands/generate-cluster.html
[clusterctl get kubeconfig]: https://cluster-api.sigs.k8s.io/clusterctl/commands/get-kubeconfig.html
[clusterctl]: https://cluster-api.sigs.k8s.io/clusterctl/overview.html
[Docker]: https://www.docker.com/
[infrastructure provider]: https://cluster-api.sigs.k8s.io/reference/glossary.html#infrastructure-provider
[kind]: https://kind.sigs.k8s.io/
[KubeadmControlPlane]: https://cluster-api.sigs.k8s.io/developer/architecture/controllers/control-plane.html
[kubectl]: https://kubernetes.io/docs/tasks/tools/install-kubectl/
[management cluster]: https://cluster-api.sigs.k8s.io/reference/glossary.html#management-cluster
[provider]: https://cluster-api.sigs.k8s.io/reference/providers.html
[provider components]: https://cluster-api.sigs.k8s.io/reference/glossary.html#provider-components
[workload cluster]: https://cluster-api.sigs.k8s.io/reference/glossary.html#workload-cluster
[Tinkerbell]: https://tinkerbell.org
