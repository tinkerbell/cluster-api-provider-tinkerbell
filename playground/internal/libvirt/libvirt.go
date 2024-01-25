package libvirt

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/tinkerbell/cluster-api-provider/playground/internal/exec"
)

type Opts struct {
	AuditWriter io.Writer
}

func (o Opts) CreateVM(name string, netBridge string, mac net.HardwareAddr) error {
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
	if o.AuditWriter != nil {
		e.AuditWriter = o.AuditWriter
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error creating: command: %v: error: %s: out: %v", fmt.Sprintf("%v %v", cmd, strings.Join(args, " ")), err, string(out))
	}

	return nil
}

// VersionGTE checks if the version of virsh is greater than or equal to the given version
func VersionGTE(version int) error {
	cmd := "virsh"
	args := []string{
		"--version",
	}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running virsh --version: %s: out: %v", err, string(out))
	}

	// virsh --version returns "X.Y.Z" where X, Y, and Z are integers
	// compare that the first number is greater than or equal to the given version
	first := strings.Split(string(out), ".")
	if len(first) < 1 {
		return fmt.Errorf("error parsing virsh --version: %s", string(out))
	}

	got, err := strconv.Atoi(first[0])
	if err != nil {
		return fmt.Errorf("error parsing virsh --version: %s", string(out))
	}
	if got >= version {
		return nil
	}

	return fmt.Errorf("virsh version %d is less than %d", got, version)
}
