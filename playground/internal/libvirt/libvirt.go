package libvirt

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

func CreateVM(name string, netBridge string, mac net.HardwareAddr) error {
	cmd := "virt-install"
	args := []string{
		"--description", "CAPT VM",
		"--ram", "2048",
		"--vcpus", "2",
		"--os-variant", "ubuntu20.04",
		"--graphics", "vnc",
		"--boot", "uefi,firmware.feature0.name=enrolled-keys,firmware.feature0.enabled=no,firmware.feature1.name=secure-boot,firmware.feature1.enabled=yes",
		"--noautoconsole",
		"--noreboot",
		"--import",
		"--connect", "qemu:///system",
	}
	args = append(args, "--name", name)
	args = append(args, "--disk", fmt.Sprintf("path=/tmp/%v-disk.img,bus=virtio,size=10,sparse=yes", name))
	args = append(args, "--network", fmt.Sprintf("bridge:%s,mac=%s", netBridge, mac.String()))
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error creating: command: %v: error: %s: out: %v", fmt.Sprintf("%v %v", cmd, strings.Join(args, " ")), err, string(out))
	}

	return nil
}
