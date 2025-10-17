# IPAM Integration for Cluster API Provider Tinkerbell

## Overview

Cluster API Provider Tinkerbell (CAPT) now supports integration with Cluster API IPAM providers for dynamic IP address allocation. This allows you to automatically allocate IP addresses from an IPAM pool and configure them on Hardware resources.

## How It Works

When a `TinkerbellMachine` is created with an IPAM pool reference:

1. CAPT creates an `IPAddressClaim` resource requesting an IP address from the specified pool
2. An IPAM provider (e.g., in-cluster IPAM) fulfills the claim by creating an `IPAddress` resource
3. CAPT reads the allocated IP address from the `IPAddress` resource
4. The IP address is automatically set on the Hardware's first network interface DHCP configuration

This seamlessly integrates with the Tinkerbell workflow, ensuring that machines are provisioned with their allocated IP addresses.

## Prerequisites

1. A working Cluster API management cluster
2. CAPT installed and configured
3. An IPAM provider installed (e.g., cluster-api-ipam-provider-in-cluster)
4. An IP pool resource created by your IPAM provider

## Usage Example

### Step 1: Create an IP Pool

First, create an IPAM pool resource. The exact format depends on your IPAM provider. Here's an example using the in-cluster IPAM provider:

```yaml
apiVersion: ipam.cluster.x-k8s.io/v1beta1
kind: InClusterIPPool
metadata:
  name: my-ip-pool
  namespace: default
spec:
  addresses:
    - 192.168.1.100-192.168.1.200
  prefix: 24
  gateway: 192.168.1.1
```

### Step 2: Reference the Pool in TinkerbellMachineTemplate

Add the `ipamPoolRef` field to your `TinkerbellMachineTemplate`:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: my-machine-template
  namespace: default
spec:
  template:
    spec:
      hardwareAffinity:
        required:
          - labelSelector:
              matchLabels:
                type: worker
      ipamPoolRef:
        apiGroup: ipam.cluster.x-k8s.io
        kind: InClusterIPPool
        name: my-ip-pool
```

### Step 3: Create the Cluster

When you create a cluster using this template, CAPT will automatically:

- Create an `IPAddressClaim` for each machine
- Wait for the IPAM provider to allocate an IP
- Configure the Hardware with the allocated IP address

```bash
kubectl apply -f my-cluster.yaml
```

### Step 4: Verify IP Allocation

Check that IP addresses have been allocated:

```bash
# List IP address claims
kubectl get ipaddressclaims

# List allocated IP addresses
kubectl get ipaddresses

# Check Hardware configuration
kubectl get hardware my-hardware-name -o jsonpath='{.spec.interfaces[0].dhcp.ip}'
```

## Hardware Configuration

When an IP is allocated via IPAM, CAPT automatically updates the Hardware resource:

```yaml
apiVersion: tinkerbell.org/v1alpha1
kind: Hardware
metadata:
  name: my-hardware
spec:
  interfaces:
    - dhcp:
        mac: "00:00:00:00:00:01"
        ip:
          address: "192.168.1.100"  # Allocated by IPAM
          netmask: "255.255.255.0"   # Derived from pool prefix
          gateway: "192.168.1.1"     # From pool configuration
        hostname: my-machine
      netboot:
        allowPXE: true
        allowWorkflow: true
```

## Cleanup

When a `TinkerbellMachine` is deleted:

1. CAPT removes the finalizer from the `IPAddressClaim`
2. The `IPAddressClaim` is deleted
3. The IPAM provider releases the IP address back to the pool
4. The `IPAddress` resource is cleaned up

This ensures proper IP address lifecycle management.

## Supported IPAM Providers

CAPT supports any IPAM provider that implements the Cluster API IPAM contract, including:

- [In-Cluster IPAM Provider](https://github.com/kubernetes-sigs/cluster-api-ipam-provider-in-cluster)
- [Nutanix IPAM Provider](https://github.com/nutanix-cloud-native/cluster-api-ipam-provider-nutanix)
- Custom IPAM providers

## Without IPAM

If you don't specify an `ipamPoolRef`, CAPT works as before - you must manually configure IP addresses in your Hardware resources:

```yaml
apiVersion: tinkerbell.org/v1alpha1
kind: Hardware
spec:
  interfaces:
    - dhcp:
        mac: "00:00:00:00:00:01"
        ip:
          address: "192.168.1.100"  # Manually configured
          netmask: "255.255.255.0"
          gateway: "192.168.1.1"
```

## Troubleshooting

### IP Not Allocated

If an IP address is not being allocated:

1. Check that the IPAM provider is running:
   ```bash
   kubectl get pods -n ipam-system
   ```

2. Check the IPAddressClaim status:
   ```bash
   kubectl describe ipaddressclaim <claim-name>
   ```

3. Check IPAM provider logs:
   ```bash
   kubectl logs -n ipam-system -l control-plane=controller-manager
   ```

### IP Pool Exhausted

If the IP pool is exhausted:

1. Check available addresses in the pool:
   ```bash
   kubectl describe inclusterippool my-ip-pool
   ```

2. Either expand the pool or remove unused machines to free up addresses

### Hardware Not Updated

If the Hardware is not being updated with the allocated IP:

1. Check TinkerbellMachine controller logs:
   ```bash
   kubectl logs -n capt-system -l control-plane=controller-manager
   ```

2. Verify the IPAddress resource was created:
   ```bash
   kubectl get ipaddress
   ```

## API Reference

### TinkerbellMachineSpec.IPAMPoolRef

```go
type TinkerbellMachineSpec struct {
    // ... other fields ...
    
    // IPAMPoolRef is a reference to an IPAM pool resource to allocate an IP address from.
    // When specified, an IPAddressClaim will be created to request an IP address allocation.
    // The allocated IP will be set on the Hardware's first interface DHCP configuration.
    // This enables integration with Cluster API IPAM providers for dynamic IP allocation.
    // +optional
    IPAMPoolRef *corev1.TypedLocalObjectReference `json:"ipamPoolRef,omitempty"`
}
```

The `IPAMPoolRef` field accepts a standard Kubernetes `TypedLocalObjectReference`:

- `apiGroup`: The API group of the IP pool resource (e.g., `ipam.cluster.x-k8s.io`)
- `kind`: The kind of the IP pool resource (e.g., `InClusterIPPool`)
- `name`: The name of the IP pool resource

## Example: Complete Cluster with IPAM

```yaml
---
apiVersion: ipam.cluster.x-k8s.io/v1beta1
kind: InClusterIPPool
metadata:
  name: production-pool
  namespace: default
spec:
  addresses:
    - 10.0.0.100-10.0.0.200
  prefix: 24
  gateway: 10.0.0.1

---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellCluster
metadata:
  name: my-cluster
  namespace: default
spec:
  controlPlaneEndpoint:
    host: 10.0.0.50
    port: 6443

---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TinkerbellMachineTemplate
metadata:
  name: my-cluster-control-plane
  namespace: default
spec:
  template:
    spec:
      hardwareAffinity:
        required:
          - labelSelector:
              matchLabels:
                type: controlplane
      ipamPoolRef:
        apiGroup: ipam.cluster.x-k8s.io
        kind: InClusterIPPool
        name: production-pool

---
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cluster
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
        - 172.25.0.0/16
    services:
      cidrBlocks:
        - 172.26.0.0/16
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta1
    kind: KubeadmControlPlane
    name: my-cluster-control-plane
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: TinkerbellCluster
    name: my-cluster
```

## Benefits

- **Automated IP Management**: No need to manually track and assign IP addresses
- **Conflict Prevention**: IPAM ensures no IP address conflicts
- **Scalability**: Easy to provision many machines without manual IP assignment
- **Integration**: Works seamlessly with existing Cluster API IPAM providers
- **Flexibility**: Optional feature - use it when needed, fallback to manual configuration otherwise
