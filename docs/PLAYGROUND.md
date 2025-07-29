# CAPT development setup with the Playground

This document describes how to set up a development environment for CAPT using the Playground.

The following repositories are required for the CAPT development setup.

- `github.com/tinkerbell/cluster-api-provider-tinkerbell`
- `github.com/tinkerbell/playground`.

## Setup

1. Fork the CAPT repository in GitHub.
1. Clone CAPT and the Playground repositories to your local machine.

    ```bash
    git clone git@github.com:<your user>/cluster-api-provider-tinkerbell.git
    git clone https://github.com/tinkerbell/playground.git
    ```

1. Create a local release in CAPT.

    ```bash
    cd cluster-api-provider-tinkerbell
    make release-local
    ```

1. In the `config.yaml`, set `capt.providerRepository` to the local release directory, minus the version. For example: `capt.providerRepository: /home/user/cluster-api-provider-tinkerbell/out/release/infrastructure-tinkerbell`

    ```bash
    cd playground/capt
    sed -i 's|providerRepository:.*|providerRepository: /home/user/cluster-api-provider-tinkerbell/out/release/infrastructure-tinkerbell|' config.yaml
    ```

1. Create the CAPT playground. Once the playground gets to the prompt waiting for user input, leave it for now and move on to the next step in this doc.

    ```bash
    cd playground/capt
    task create-playground
    ```

1. Deploy the local CAPT code.

    ```bash
    cd cluster-api-provider-tinkerbell
    export KUBECONFIG=<playground repo>/capt/output/kind.kubeconfig
    tilt up --stream
    ```

1. Enter `y` in the CAPT Playground prompt and follow the post creation instructions.

## Pivot the CAPI and CAPT management components to the created Tinkerbell cluster

To test and play with the CAPI pivot process, move the CAPI and CAPT management components from the KinD cluster to the new Tinkerbell cluster.

```bash
task pivot
```

### Understanding the pivot process

The pivot process follows the CAPI process defined in the [CAPI documentation](https://cluster-api.sigs.k8s.io/clusterctl/commands/move#bootstrap--pivot). The following are example steps of what the command `task pivot` does. Inspect the playground file `Taskfile-capi-pivot.yaml`, run `task pivot --dry`, and see the output after running `task pivot` to see the actual commands that are executed.

Example steps:

1. Create the CAPT Playground cluster:

   ```bash
   task create-playground
   ```

1. Install the Tinkerbell stack in the new cluster:

   ```bash
   export KUBECONFIG=output/capt-playground.kubeconfig
   TRUSTED_PROXIES=$(kubectl get nodes -o jsonpath='{.items[*].spec.podCIDR}' | tr ' ' ',')
   LB_IP=172.18.10.84
   ARTIFACTS_FILE_SERVER=http://172.18.10.85:7173
   helm upgrade --install tinkerbell oci://ghcr.io/tinkerbell/charts/tinkerbell --version v0.19.2-5d22212c --create-namespace --namespace tinkerbell --wait --set "trustedProxies={${TRUSTED_PROXIES}}" --set "publicIP=$LB_IP" --set "artifactsFileServer=$ARTIFACTS_FILE_SERVER"
   ```

1. Initialize CAPI in the cluster:

   ```bash
   export KUBECONFIG=output/capt-playground.kubeconfig
   export TINKERBELL_IP=172.18.10.84
   clusterctl --config output/clusterctl.yaml init --infrastructure tinkerbell
   ```

1. Move the CAPI resources from the KinD cluster to the new cluster:

   ```bash
   export KUBECONFIG=output/kind.kubeconfig
   clusterctl move --to-kubeconfig="output/capt-playground.kubeconfig" --config output/clusterctl.yaml --kubeconfig output/kind.kubeconfig -n tinkerbell
   ```

1. Delete the KinD cluster:

   ```bash
    kind delete cluster --name capt-playground
    ```
