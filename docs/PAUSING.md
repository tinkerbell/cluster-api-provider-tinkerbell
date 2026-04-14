# Pausing Clusters and Machines

Pausing prevents CAPT from reconciling infrastructure resources. This is useful during maintenance, debugging, or when you need to make manual changes without the controller reverting them.

## Pausing a Cluster

Pausing a `Cluster` pauses all associated infrastructure resources (`TinkerbellCluster` and `TinkerbellMachine`).

```sh
kubectl annotate cluster <cluster-name> cluster.x-k8s.io/paused=""
```

Or set `spec.paused` on the Cluster:

```sh
kubectl patch cluster <cluster-name> --type merge -p '{"spec":{"paused":true}}'
```

## Pausing a Single Machine

To pause a specific `TinkerbellMachine` without affecting the rest of the cluster:

```sh
kubectl annotate tinkerbellmachine <machine-name> cluster.x-k8s.io/paused=""
```

## Checking Pause Status

When paused, a `Paused` condition is set on the resource:

```sh
kubectl get tinkerbellcluster <name> -o jsonpath='{.status.conditions[?(@.type=="Paused")]}'
kubectl get tinkerbellmachine <name> -o jsonpath='{.status.conditions[?(@.type=="Paused")]}'
```

A paused resource shows `status: "True"` with `reason: Paused`.

## Unpausing

Remove the annotation:

```sh
kubectl annotate cluster <cluster-name> cluster.x-k8s.io/paused-
kubectl annotate tinkerbellmachine <machine-name> cluster.x-k8s.io/paused-
```

Or unset `spec.paused`:

```sh
kubectl patch cluster <cluster-name> --type merge -p '{"spec":{"paused":false}}'
```

The `Paused` condition will update to `status: "False"` with `reason: NotPaused`, and reconciliation resumes.

## What Pausing Blocks

- Creation and deletion of Tinkerbell Templates, Workflows, and BMC Jobs
- Hardware selection and provisioning
- Provider reconciliation and status progression on `TinkerbellCluster` and `TinkerbellMachine`
- Deletion reconciliation (finalizer removal is deferred until unpaused)

Note: The `Paused` condition itself is always written and updated, even while paused. Only provider-specific reconciliation and status progression are deferred.
