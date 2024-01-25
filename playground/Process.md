# Process

## Inputs

* total number of hardware objects
* number of control planes
* number of worker nodes
* Kubernetes version
* Tinkerbell stack version

## Dependencies

* Libvirtd >= libvirtd (libvirt) 8.0.0
* Docker >= 24.0.7
* Helm >= v3.13.1
* KinD >= v0.20.0
* clusterctl >= v1.6.0
* kubectl >= v1.28.2
* virt-install >= 4.0.0

### Hardware Dependencies

* 60GB of free and very fast disk space (etcd is very disk io sensitive)
* 8GB of free RAM
* 4 CPU cores

## Phase 1: Deploy dependencies

1. Create KinD cluster.
1. Deploy Tinkerbell stack.

## Phase 1: Generate

1. Generate MAC address.
1. Generate IP address.
1. Generate Hostname.
1. Generate Hardware object.
   * Add labels (`capt-node-role=control-plane`, `capt-node-role=worker-node`)
   * IPAM
   * etc
1. Generate BMC objects.
   * machine and secret
   * docker container name seems to resolve in the Rufio pod.
1. Generate capi cluster objects.
   * requires:

   ```bash
   mkdir -p ~/.cluster-api
   cat >> ~/.cluster-api/clusterctl.yaml <<EOF
   providers:
     - name: "tinkerbell"
       url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/v0.4.0/infrastructure-components.yaml"
       type: "InfrastructureProvider"
   EOF

   TINKERBELL_IP=172.18.18.17 clusterctl init --infrastructure tinkerbell
   ```

   * Generate CAPI yamls.

   ```bash
   CONTROL_PLANE_VIP=172.18.18.17 POD_CIDR=172.25.0.0/16 clusterctl generate cluster playground --kubernetes-version v1.23.5 --control-plane-machine-count=1 --worker-machine-count=2 --target-namespace=tink-system --write-to playground.yaml
   ```

   * Modify and add hardwareAffinity to TinkerbellMachineTemplate for control-plane and worker-node.

   ```bash
   kubectl kustomize -o output/playground.yaml
   ```

## Phase 2: VM creation

1. Discover KinD network bridge name.

    ```bash
    network_id=$(docker network inspect -f {{.Id}} kind)
    bridge_name="br-${network_id:0:12}"
    brctl show $bridge_name >/dev/null 2>&1 || echo "bridge $bridge_name does not exist"
    ```

1. Create VMs.
   * `virt-install`
   * Use KinD bridge and MAC address.

## Phase 3: Virtual BMC setup

1. Start the Virtual BMC.
1. Add/register VMs to Virtual BMC.
1. Start the virtual BMC for all machines.

```bash
docker run -it --rm -v ~/.kube/config:/root/.kube/config --network kind -v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock -v /var/run/docker.sock:/var/run/docker.sock capt-playground

for i in {1..4}; do echo $i; docker exec -it virtualbmc vbmc add --username admin --password password --port "623$i" --no-daemon "node$i"; done
for i in {1..4}; do echo $i; docker exec -it virtualbmc vbmc start "node$i"; done
```

## Phase 4: Apply

> NOTE: Manual steps for now.

1. update capt image. Needed until we have a new release.

   ```bash
   export KUBECONFIG=output/kind.kubeconfig
   kubectl set image deployment/capt-controller-manager -n capt-system manager=reg.weinstocklabs.com/tinkerbell/capt-amd64:latest
   ```

1. Apply CAPI/CAPT cluster objects.

   ```bash
   kubectl apply -f output/playground.yaml
   ```

## Phase 5: Post cluster creation

1. Get kubeconfig.

   ```bash
   export KUBECONFIG=output/kind.kubeconfig
   clusterctl get kubeconfig playground -n tink-system > playground.kubeconfig 
   ```

1. Install CNI.

   ```bash
   export KUBECONFIG=playground.kubeconfig
   cilium install --version 1.14.5
   ```

## TODO

1. Clean up

   ```bash
   kind delete cluster --name playground
   rm -rf output/
   docker rm -f virtualbmc
   for i in {1..4}; do echo $i; sudo virsh destroy "node$i"; sudo virsh undefine "node$i" --remove-all-storage --nvram; done
   ```

1. Kubeconfig retrieval
1. CNI installation
1. CAPT development (tilt like, maybe tilt)
1. capi init (maybe CAPI operator helm chart)

> CAPI wants to write files to disk. No docs on this. Had to specify all XDG paths to get this to not write to privileged paths.

* /home/tink/.config/cluster-api/version.yaml
* Error: unable to verify clusterctl version: unable to write version state file: mkdir /etc/xdg/cluster-api: permission denied
