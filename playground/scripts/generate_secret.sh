#!/bin/bash

# Generate secret. All machines share the same secret. The only customization is the namespace, user name, and password.

function main() {
    declare OUTPUT_DIR="$1"
    declare NAMESPACE="$2"
    declare USER_NAME="$3"
    declare PASSWORD="$4"

    export NAMESPACE="$NAMESPACE"
    export BMC_USER_BASE64="$(echo -n "$USER_NAME" | base64)"
    export BMC_PASS_BASE64="$(echo -n "$PASSWORD" | base64)"

    envsubst "$(printf '${%s} ' $(env | cut -d'=' -f1))" < templates/bmc-secret.tmpl > "$OUTPUT_DIR"/bmc-secret.yaml
}

main "$@"

