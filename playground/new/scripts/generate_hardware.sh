#!/bin/bash

# Generate hardware

# Inputs: TOTAL_HARDWARE, CONTROL_PLANE_NODES, WORKER_NODES, GATEWAY_IP, NODE_IP_BASE, OUTPUT_DIR


function generate_mac() {
  declare -i NODE_NAME="$1"

  echo "$NODE_NAME" | md5sum|sed 's/^\(..\)\(..\)\(..\)\(..\)\(..\).*$/02:\1:\2:\3:\4:\5/'
}

function create_hardware_yaml_file() {
  declare OUTPUT_DIR="$1"
  declare NODE_NAME="$2"

  # this gets all the environment variables and substitutes them in the template
  envsubst "$(printf '${%s} ' $(env | cut -d'=' -f1))" < templates/hardware.tmpl > "$OUTPUT_DIR"/hardware-"$NODE_NAME".yaml
}


function main() {
  # Generate hardware
  declare -i CONTROL_PLANE_NODES="$1"
  declare -i WORKER_NODES="$2"
  declare -i SPARE_NODES="$3"
  declare GATEWAY_IP="$4"
  declare NODE_IP_BASE="$5"
  declare OUTPUT_DIR="$6"
  declare CLEANUP_OLD="$7"

  IP_LAST_OCTET=$(echo "$NODE_IP_BASE" | cut -d. -f4)
  export GATEWAY_IP="$GATEWAY_IP"

  if [ "$CLEANUP_OLD" == "true" ]; then
    rm -f "$OUTPUT_DIR"/hardware*.yaml
  fi

  for i in $(seq 1 "$CONTROL_PLANE_NODES"); do
    export NODE_ROLE="control-plane"
    export NODE_NAME="node$i"
    v=$(echo "$NODE_IP_BASE" | awk -F"." '{print $1"."$2"."$3}').$((IP_LAST_OCTET + i))
    export NODE_IP="$v"
    export NODE_MAC=$(generate_mac "$NODE_NAME")
    create_hardware_yaml_file "$OUTPUT_DIR" "$NODE_NAME"
    unset NODE_ROLE
    unset NODE_NAME
    unset NODE_IP
    unset NODE_MAC
  done

  for i in $(seq 1 "$WORKER_NODES"); do
    export NODE_ROLE="worker"    
    export NODE_NAME="node$((CONTROL_PLANE_NODES + i))"
    v=$(echo "$NODE_IP_BASE" | awk -F"." '{print $1"."$2"."$3}').$((IP_LAST_OCTET + CONTROL_PLANE_NODES + i))
    export NODE_IP="$v"
    export NODE_MAC=$(generate_mac node-$((CONTROL_PLANE_NODES + i)))
    create_hardware_yaml_file "$OUTPUT_DIR" "$NODE_NAME" '$NODE_ROLE,$NODE_NAME,$NODE_IP,$GATEWAY_IP,$NODE_MAC'
    unset NODE_ROLE
    unset NODE_NAME
    unset NODE_IP
    unset NODE_MAC
  done

  for i in $(seq 1 "$SPARE_NODES"); do
    export NODE_ROLE="spare"
    export NODE_NAME="node$((CONTROL_PLANE_NODES + WORKER_NODES + i))"
    v=$(echo "$NODE_IP_BASE" | awk -F"." '{print $1"."$2"."$3}').$((IP_LAST_OCTET + CONTROL_PLANE_NODES + WORKER_NODES + i))
    export NODE_IP="$v"
    export NODE_MAC=$(generate_mac node-$((CONTROL_PLANE_NODES + WORKER_NODES + i)))
    create_hardware_yaml_file "$OUTPUT_DIR" "$NODE_NAME" '$NODE_ROLE,$NODE_NAME,$NODE_IP,$GATEWAY_IP,$NODE_MAC'
    unset NODE_ROLE
    unset NODE_NAME
    unset NODE_IP
    unset NODE_MAC
  done

}

main "$@"