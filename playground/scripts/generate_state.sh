#!/bin/bash
# This script generates the state data needed for creating the CAPT playground.

# state file spec
cat <<EOF > /dev/null
---
clusterName: "capt-playground"
outputDir: "/home/tink/repos/tinkerbell/cluster-api-provider-tinkerbell/playground/new/output"
namespace: "tink-system"
counts:
  controlPlanes: 1
  workers: 2
  spares: 1
versions:
  capt: 0.5.3
  chart: 0.4.4
  kube: v1.28.8
  os: 22.04
os:
  registry: reg.weinstocklabs.com/tinkerbell/cluster-api-provider-tinkerbell
  distro: ubuntu
  sshKey: ""
vm:
  baseName: "machine"
  names:
    - machine1
    - machine2
    - machine3
    - machine4
  details:
    machine1:
      mac: 02:ab:14:52:5f:c9
      bmc:
        port: 6231
    machine2:
      mac: 02:fd:d0:11:16:85
      bmc:
        port: 6232
    machine3:
      mac: 02:aa:58:69:f7:de
      bmc:
        port: 6233
    machine4:
      mac: 02:ca:c3:59:f0:51
      bmc:
        port: 6234
virtualBMC:
  containerName: "virtualbmc"
  image: ghcr.io/jacobweinstock/virtualbmc
  user: "root"
  pass: "calvin"
totalNodes: 4
kind:
  kubeconfig: /home/tink/repos/tinkerbell/cluster-api-provider-tinkerbell/playground/new/output/kind.kubeconfig
EOF

set -euo pipefail

function generate_mac() {
  declare NODE_NAME="$1"

  echo "$NODE_NAME" | md5sum|sed 's/^\(..\)\(..\)\(..\)\(..\)\(..\).*$/02:\1:\2:\3:\4:\5/'
}

function main() {
    # read in the config.yaml file and populate the .state file
    declare CONFIG_FILE="$1"
    declare STATE_FILE="$2"

    # update outputDir to be a fully qualified path    
    output_dir=$(yq eval '.outputDir' "$CONFIG_FILE")
    if [[ "$output_dir" = /* ]]; then
        echo
    else
        current_dir=$(pwd)
        output_dir="$current_dir/$output_dir"
    fi
    config_file=$(realpath "$CONFIG_FILE")
    state_file="$STATE_FILE"

    cp -a "$config_file" "$state_file"
    yq e -i '.outputDir = "'$output_dir'"' "$state_file"

    # totalNodes
    total_nodes=$(($(yq eval '.counts.controlPlanes' "$state_file") + $(yq eval '.counts.workers' "$state_file") + $(yq eval '.counts.spares' "$state_file")))
    yq e -i ".totalNodes = $total_nodes" "$state_file"

    # populate vmNames
    base_name=$(yq eval '.vm.baseName' "$state_file")
    base_ipmi_port=6230
    for i in $(seq 1 $total_nodes); do
        name="$base_name$i"
        mac=$(generate_mac "$name")
        if [[ "$(yq '.vm.names | length' "$state_file")" -eq 0 ]]; then
            yq e -i ".vm.names = [\"$name\"]" "$state_file"
        fi
        if ! $(yq '.vm.names | any_c(. == "'$name'")' "$state_file"); then
            yq e -i ".vm.names += [\"$name\"]" "$state_file"            
        fi
        yq e -i ".vm.details.$name.mac = \"$mac\"" "$state_file"
        yq e -i ".vm.details.$name.bmc.port = $(($base_ipmi_port + $i))" "$state_file"
        # set the node role
        if [[ $i -le $(yq eval '.counts.controlPlanes' "$state_file") ]]; then
            yq e -i ".vm.details.$name.role = \"control-plane\"" "$state_file"
        elif [[ $i -le $(($(yq eval '.counts.controlPlanes' "$state_file") + $(yq eval '.counts.workers' "$state_file"))) ]]; then
            yq e -i ".vm.details.$name.role = \"worker\"" "$state_file"
        else
            yq e -i ".vm.details.$name.role = \"spare\"" "$state_file"
        fi
        unset name
        unset mac
    done

    # populate kind.kubeconfig
    yq e -i '.kind.kubeconfig = "'$output_dir'/kind.kubeconfig"' "$state_file"

    # populate the expected OS version in the raw image name (22.04 -> 2204)
    os_version=$(yq eval '.versions.os' "$state_file")
    os_version=$(echo "$os_version" | tr -d '.')
    yq e -i '.os.version = "'$os_version'"' "$state_file"
}

main "$@"
