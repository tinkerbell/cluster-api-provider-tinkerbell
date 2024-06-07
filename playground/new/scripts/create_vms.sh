#!/bin/bash

# Create VMs

function main() {
    declare BRIDGE_NAME="$1"
    declare OUTPUT_DIR="$2"

    # for each hardware-*.yaml file, create a VM
    for f in "$OUTPUT_DIR"/hardware-*.yaml; do
        # get the node name from the file name
        NODE_NAME=$(yq .metadata.name "$f")
        MAC_ADDR=$(yq .spec.metadata.instance.id "$f")
        # create the VM
        virt-install \
        --description "CAPT VM" \
        --ram 2048 --vcpus 2 \
        --os-variant "ubuntu20.04" \
        --graphics "vnc" \
        --boot "uefi,firmware.feature0.name=enrolled-keys,firmware.feature0.enabled=no,firmware.feature1.name=secure-boot,firmware.feature1.enabled=yes" \
        --noautoconsole \
        --noreboot \
        --import \
        --connect "qemu:///system" \
        --name "$NODE_NAME" \
        --disk "path=/tmp/$NODE_NAME-disk.img,bus=virtio,size=10,sparse=yes" \
        --network "bridge:$BRIDGE_NAME,mac=$MAC_ADDR"
    done

}

main "$@"