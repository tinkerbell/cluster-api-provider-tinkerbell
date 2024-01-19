package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"

	"github.com/tinkerbell/cluster-api-provider/playground/internal/exec"
)

const binary = "docker"

type Args struct {
	Cmd                  string
	Detach               bool
	Autoremove           bool
	Network              string
	BindMounts           map[string]string
	Image                string
	Name                 string
	OutputFormat         string
	AdditionalPrefixArgs []string
	AdditionalSuffixArgs []string
	AuditWriter          io.Writer
}

type Opts struct {
	AuditWriter io.Writer
}

// RunCommand runs a docker command with the given args
func RunCommand(ctx context.Context, c Args) (string, error) {
	cmd := binary
	args := []string{c.Cmd}
	args = append(args, c.AdditionalPrefixArgs...)
	if c.Name != "" {
		args = append(args, "--name", c.Name)
	}
	if c.Detach {
		args = append(args, "-d")
	}
	if c.Autoremove {
		args = append(args, "--rm")
	}
	if c.Network != "" {
		args = append(args, "--network", c.Network)
	}
	for hostPath, containerPath := range c.BindMounts {
		args = append(args, "-v", fmt.Sprintf("%s:%s", hostPath, containerPath))
	}
	if c.OutputFormat != "" {
		args = append(args, "--format", c.OutputFormat)
	}
	if c.Image != "" {
		args = append(args, c.Image)
	}
	args = append(args, c.AdditionalSuffixArgs...)

	e := exec.CommandContext(context.Background(), cmd, args...)
	if c.AuditWriter != nil {
		e.AuditWriter = c.AuditWriter
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run container: cmd: %v err: %w: out: %s", fmt.Sprintf("[%v %v]", cmd, strings.Join(args, " ")), err, out)
	}

	return string(out), nil
}

// IPv4SubnetFrom returns the subnet mask from the given docker network
func (o Opts) IPv4SubnetFrom(dockerNet string) (net.IPMask, error) {
	/*
		docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}},{{end}}'
		result: 172.20.0.0/16,fc00:f853:ccd:e793::/64,
	*/
	args := Args{
		Cmd:                  "network",
		OutputFormat:         "'{{range .IPAM.Config}}{{.Subnet}},{{end}}'",
		AdditionalPrefixArgs: []string{"inspect", dockerNet},
		AuditWriter:          o.AuditWriter,
	}
	out, err := RunCommand(context.Background(), args)
	if err != nil {
		return nil, fmt.Errorf("error getting subnet: %s: out: %v", err, string(out))
	}

	ot := strings.Trim(strings.Trim(string(out), "\n"), "'")
	subnets := strings.Split(ot, ",")
	for _, s := range subnets {
		_, ipnet, err := net.ParseCIDR(s)
		if err == nil {
			if ipnet.IP.To4() != nil {
				return ipnet.Mask, nil
			}
		}
	}

	return nil, fmt.Errorf("unable to determine docker network subnet mask, err from command: %s: stdout: %v", err, string(out))
}

func (o Opts) IPv4GatewayFrom(dockerNet string) (netip.Addr, error) {
	/*
		docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}},{{end}}'
		result: 172.20.0.1,
	*/
	args := Args{
		Cmd:                  "network",
		OutputFormat:         "'{{range .IPAM.Config}}{{.Gateway}},{{end}}'",
		AdditionalPrefixArgs: []string{"inspect", dockerNet},
		AuditWriter:          o.AuditWriter,
	}
	out, err := RunCommand(context.Background(), args)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("error getting gateway: %w", err)
	}

	ot := strings.Trim(strings.Trim(string(out), "\n"), "'")
	subnets := strings.Split(ot, ",")
	for _, s := range subnets {
		ip, err := netip.ParseAddr(s)
		if err == nil && ip.Is4() {
			return ip, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("unable to determine docker network gateway, err from command: %s: stdout: %v", err, string(out))
}

func (o Opts) LinuxBridgeFrom(dockerNet string) (string, error) {
	/*
		network_id=$(docker network inspect -f {{.Id}} kind)
		    bridge_name="br-${network_id:0:11}"
		    brctl show $bridge_name
	*/
	args := Args{
		Cmd:                  "network",
		OutputFormat:         "'{{.Id}}'",
		AdditionalPrefixArgs: []string{"inspect"},
		AdditionalSuffixArgs: []string{dockerNet},
		AuditWriter:          o.AuditWriter,
	}
	out, err := RunCommand(context.Background(), args)
	if err != nil {
		return "", fmt.Errorf("error getting network id: %w", err)
	}
	bridgeID := string(out)[:13]
	bridgeID = strings.Trim(bridgeID, "'")
	bridgeName := fmt.Sprintf("br-%s", bridgeID)
	// TODO: check if bridge exists

	return bridgeName, nil
}
