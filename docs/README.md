## Running development version of CAPT using existing Tinkerbell instance

If you have Tinkerbell running in your environment, you can use it for CAPT.

### Requirements

Here is the list of required components to use CAPT:

- Existing Tinkerbell installation running at least versions mentioned in [sandbox](https://github.com/tinkerbell/sandbox/tree/v0.5.0), this guide assumes deployment using the sandbox with an IP address of 192.168.1.1, so modifications will be needed if Tinkerbell was deployed with a different method or if the IP address is different.
- A Kubernetes cluster which pods has access to your Tinkerbell instance.
- At least one Hardware available with DHCP IP address configured on first interface and with proper metadata configured
- `git` binary
- `tilt` binary
- `kubectl` binary
- `clusterctl` binary
- `go` binary

Required metadata for hardware:
- metadata.facility.facility_code is set (default is "onprem")
- metadata.instance.id is set (should match the hardware's id)
- metadata.instance.hostname is set
- metadata.instance.storage.disks is set and contains at least one device matching an available disk on the system

### Workaround for 8000 bytes template limit

CAPT creates rather large templates for machine provisioning, so with regular Tinkerbell
installation you will probably hit this [limit](https://github.com/tinkerbell/tink/issues).

To workaround that, run the following SQL command in your Tinkerbell database:
```sql
drop trigger events_channel ON events;
```

**WARNING: This will disable events streaming feature!**

If you use Tinkerbell [sandbox](https://github.com/tinkerbell/sandbox), you can run the following command
in your `deploy/` directory:
```sh
PGPASSWORD=tinkerbell docker-compose exec db psql -U tinkerbell -c 'drop trigger events_channel ON events;'
```

### Add a link-local address for Hegel

If your sandbox is running on an Ubuntu system, you can edit `/etc/netplan.<devicename>.yaml`, add `169.254.169.254/16` to the addresses, and run `netplan apply`

### Replace OSIE with Hook

If you can use the default hook image, then copy http://s.gianarb.it/tinkie/tinkie-main.tar.gz to your `deploy/state/webroot/misc/osie/current` directory. Otherwise, follow the directions at: https://github.com/tinkerbell/hook#how-to-use-hook-with-sandbox

### Mirror the necessary Tinkerbell actions to the registry

```sh
IMAGES=(
  image2disk:v1.0.0
  writefile:v1.0.0
  kexec:v1.0.0
)
REGISTRY_IP="192.168.1.1"
for IMAGE in "${IMAGES[@]}"; do
  docker pull quay.io/tinkerbell-actions/$IMAGE
  docker tag quay.io/tinkerbell-actions/$IMAGE $REGISTRY_IP/quay.io/tinkerbell-actions/$IMAGE
  docker push $REGISTRY_IP/quay.io/tinkerbell-actions/$IMAGE
done
```

### Prebuild the Kubernetes image

```
git clone https://github.com/detiber/image-builder
cd image-builder/images/capi
# This can take a while. ~30min on an i3 with 32GB memory
make build-raw-all
# copy output/ubuntu-1804-kube-v1.18.15.gz to your `deploy/state/webroot` directory
```

To build a different version, you can modify the image-builder configuration following the documentation here: https://image-builder.sigs.k8s.io/capi/capi.html and here: https://image-builder.sigs.k8s.io/capi/providers/raw.html

### Deploying CAPT

To run CAPT, we're going to use `tilt`.

First, make sure your `kubeconfig` points to the right Kubernetes cluster.

Take a note of name of your `kubeconfig` context, which will be used in Tilt configuration.

You can get current context name using the following command:
```sh
kubectl config get-contexts  | awk '/^*/ {print $2}'
```

Then, run the following commands to clone code we're going to run:
```sh
git clone https://github.com/kubernetes-sigs/cluster-api
git clone git@github.com:tinkerbell/cluster-api-provider-tink.git
cd ../cluster-api
```

Now, create a configuration file for Tilt. You can run the command below to create a sample config file,
then replace placeholders with actual values:
```sh
cat <<EOF > tilt-settings.json
{
  "default_registry": "quay.io/<your username>",
  "provider_repos": ["../cluster-api-provider-tink"],
  "enable_providers": ["tinkerbell", "kubeadm-bootstrap", "kubeadm-control-plane"],
  "allowed_contexts": ["<your kubeconfig context to use"],
  "kustomize_substitutions": {
    "TINKERBELL_GRPC_AUTHORITY": "192.168.1.1:42113",
    "TINKERBELL_CERT_URL": "http://192.168.1.1:42114/cert",
    "TINKERBELL_IP": "192.168.1.1"
  }
}
EOF
```

Finally, run Tilt to deploy CAPI and CAPT to your cluster using the command below:
```sh
tilt up
```

You can now open a webpage printed by Tilt to see the progress on the deployment.

### Adding Hardware objects to your cluster

Before we create a workload cluster, we must register some of Tinkerbell Hardware in Kubernetes to make it visible
by CAPT controllers.

List hardware you have available in Tinkerbell e.g. using `tink hardware list` command and save Hardware UUIDs which
you would like to use.

Then, create similar YAML file, which we later apply on the cluster:
```yaml
kind: Hardware
apiVersion: tinkerbell.org/v1alpha1
metadata:
  name: first-hardware
spec:
  id: <put hardware ID here>
---
kind: Hardware
apiVersion: tinkerbell.org/v1alpha1
metadata:
  name: second-hardware
spec:
  id: <put hardware ID here>
```

Now, apply created YAML file on your cluster.

At least one Hardware is required to create a controlplane machine. This guide uses 2 Hardwares, one for controlplane
machine and one for worker machine.

**NOTE: CAPT expects Hardware to have DHCP IP address configured on first interface of the Hardware. This IP will
be then used for Node Internal IP.**

To confirm that your Hardware entries are correct, run the following command:
```sh
kubectl describe hardware
```

In the output, you should be able to find MAC address and IP addresses of the hardware.

### Creating workload clusters

With all the steps above, we can now create a workload cluster.

So, let's start with generating the configuration for your cluster using the command below:
```sh
POD_CIDR=172.25.0.0/16 clusterctl config cluster capi-quickstart --from templates/cluster-template.yaml --kubernetes-version=v1.18.5 --control-plane-machine-count=1 --worker-machine-count=1 > test-cluster.yaml
```

Note, the POD_CIDR is overridden above to avoid conflicting with the default assumed IP address of the Tinkerbell host (192.168.1.1).

Inspect the new configuration generated in `test-cluster.yaml` and modify it as needed.

Finally, run the following command to create a cluster:
```sh
kubectl apply -f test-cluster.yaml
```

### Observing cluster provisioning

Few seconds after creating a workload cluster, you should see some log messages in Tilt tab with CAPT that IP address has been selected for controlplane machine etc.

If you list your Hardware now with labels, you should see which Hardware has been selected by CAPT controllers:
```sh
kubectl get hardware --show-labels
```

You should also be able to list and describe the created workflows for machine provisioning using the commands below:
```sh
kubectl get workflows
kubectl describe workflows
```

Once workflows are created, make sure your machines boot from the network to pick up new Workflow.

In the output of commands above, you can see status of provisioning workflows. If everything goes well, reboot step should be the last step you can see.

You can also check general cluster provisioning status using the commands below:
```sh
kubectl get kubeadmcontrolplanes
kubectl get machines
```

### Getting access to workload cluster

To finish cluster provisioning, we must get access to it and install a CNI plugin. In this guide we will use Cilium. Cilium was chosen to avoid conflicts with the default assumed IP address for Tinkerbell (192.168.1.1)

Run the following command to fetch `kubeconfig` for your workload cluster:
```sh
clusterctl get kubeconfig capi-quickstart > kubeconfig-workload
```

Now you can apply Cilium using the command below:
```sh
KUBECONFIG=kubeconfig-workload kubectl create -f https://raw.githubusercontent.com/cilium/cilium/v1.9/install/kubernetes/quick-install.yaml
```

At this point your workload cluster should be ready for other deployments.

### Cleaning up

To remove created cluster resources, run the following command:
```sh
kubectl delete -f test-cluster.yaml
```

Right now CAPT does not de-provision the hardware when cluster is removed but makes Hardware available again for other clusters. To make sure machines can be provisioned again, securely wipe their disk and reboot them.
