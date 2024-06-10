# CAPT Playground

The CAPT playground is a tool that will create a local CAPT deployment. This includes a Kubernetes cluster (KinD), the Tinkerbell stack, all CAPI and CAPT components, Virtual machines that will be used to create a workload cluster, and a Virtual BMC to manage the VMs.

Start by reviewing and installing the [prerequisites] and understanding and customizing the [configuration](config.yaml) as needed.

## Prerequisites

### Binaries

* Libvirtd >= libvirtd (libvirt) 8.0.0
* Docker >= 24.0.7
* Helm >= v3.13.1
* KinD >= v0.20.0
* clusterctl >= v1.6.0
* kubectl >= v1.28.2
* virt-install >= 4.0.0
* task >= 3.37.2

### Hardware

* at least 60GB of free and very fast disk space (etcd is very disk io sensitive)
* at least 8GB of free RAM
* at least 4 CPU cores

## Usage

Create the CAPT playground:

```bash
task create-playground
```

Delete the CAPT playground:

```bash
task delete-playground
```

### Standalone

```bash
capt-playground -h
```

### Docker

```bash
docker build -t capt-playground .
docker run -it --rm --network host -v /tmp:/tmp -v /var/run/docker.sock:/var/run/docker.sock -v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock --name capt-playground capt-playground
capt-playground -h
```

### Apply the cluster spec

Apply CAPI/CAPT cluster objects.

```bash
kubectl apply -f output/playground.yaml
```

Wait for the cluster to be ready.

First wait for the control plane(s) to be ready.

```bash
# ready means DESIRED equal REPLICAS and INITIALIZED is true
kubectl get -n tink-system kubeadmcontrolplanes -o wide -w
```

Then wait for the worker nodes to be ready.

```bash
# ready means DESIRED equal REPLICAS
kubectl get machinedeployment -n tink-system -o wide -w
```

Then get the new clusters kubeconfig.

```bash
export KUBECONFIG=output/kind.kubeconfig
clusterctl get kubeconfig playground -n tink-system > playground.kubeconfig 
```

Install a CNI.

```bash
export KUBECONFIG=playground.kubeconfig
cilium install --version 1.14.5
```

Check the cluster is ready.

```bash
kubectl get nodes
```

## Known Issues

### DNS issue

KinD on Ubuntu has a known issue with DNS resolution in KinD pod containers. This affect the Download of HookOS in the Tink stack helm deployment. There are a few [known workarounds](https://github.com/kubernetes-sigs/kind/issues/1594#issuecomment-629509450). The recommendation for the CAPT playground is to add DNS nameservers to Docker's `daemon.json` file. This can be done by adding the following to `/etc/docker/daemon.json`:

```json
{
  "dns": ["1.1.1.1"]
}
```

Then restart Docker:

```bash
sudo systemctl restart docker
```
