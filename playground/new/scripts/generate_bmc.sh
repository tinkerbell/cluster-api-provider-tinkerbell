#!/bin/bash

function create_bmc_machine_yaml_file() {
  declare OUTPUT_DIR="$1"
  declare NODE_NAME="$2"

  # this gets all the environment variables and substitutes them in the template
  envsubst "$(printf '${%s} ' $(env | cut -d'=' -f1))" < templates/bmc-machine.tmpl > "$OUTPUT_DIR"/bmc-machine-"$NODE_NAME".yaml
}

function main() {
    declare -i NUM_TOTAL_HARDWARE="$1"
    declare -i BMC_BASE_PORT="$2"
    declare BMC_IP="$3"
    declare NAMESPACE="$4"
    declare OUTPUT_DIR="$5"
    declare CLEANUP_OLD="$6"

    if [ "$CLEANUP_OLD" == "true" ]; then
        rm -f "$OUTPUT_DIR"/bmc-machine*.yaml
    fi

    export BMC_IP="$BMC_IP"
    export NAMESPACE="$NAMESPACE"

    for i in $(seq 1 "$NUM_TOTAL_HARDWARE"); do
        export BMC_PORT=$((BMC_BASE_PORT + i))
        export NODE_NAME="node$i"
        create_bmc_machine_yaml_file "$OUTPUT_DIR" "$NODE_NAME"
        unset BMC_PORT
        unset BMC_NAME
    done
}

main "$@"