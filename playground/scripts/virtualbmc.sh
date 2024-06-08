#!/bin/bash

set -euo pipefail

# This script will registry and start virtual bmc entries in a running virtualbmc container

function main() {
    declare -r STATE_FILE="$1"
    declare -r OUTPUT_DIR=$(yq eval '.outputDir' "$STATE_FILE")

    username=$(yq eval '.virtualBMC.user' "$STATE_FILE")
    password=$(yq eval '.virtualBMC.pass' "$STATE_FILE")

    while IFS=$',' read -r name port; do
        docker exec virtualbmc vbmc add --username "$username" --password "$password" --port "$port" "$name"
        docker exec virtualbmc vbmc start "$name"
    done < <(yq e '.vm.details.[] | [key, .bmc.port] | @csv' "$STATE_FILE")

}

main "$@"