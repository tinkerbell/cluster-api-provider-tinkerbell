package docker

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

type VirtualBMC struct {
	Image         string
	Network       string
	ContainerName string
	LibvirtSocket string
	BMCInfo       []BMCInfo
	AuditWriter   io.Writer
}

type BMCInfo struct {
	Username string
	Password string
	Hostname string
	Port     string
}

func (v VirtualBMC) RunVirtualBMCContainer(ctx context.Context) (netip.Addr, error) {
	/*
		docker run -d --rm --network kind -v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock --name virtualbmc capt-playground:v2
	*/
	args := Args{
		Cmd:        "run",
		Detach:     true,
		Network:    v.Network,
		Autoremove: true,
		BindMounts: map[string]string{
			fmt.Sprintf("%s-ro", v.LibvirtSocket): "/var/run/libvirt/libvirt-sock-ro",
			v.LibvirtSocket:                       "/var/run/libvirt/libvirt-sock",
		},
		Name:        v.ContainerName,
		Image:       v.Image,
		AuditWriter: v.AuditWriter,
	}
	if out, err := RunCommand(context.Background(), args); err != nil {
		return netip.Addr{}, fmt.Errorf("out: %s, err: %w", string(out), err)
	}

	// get the IP of the container
	args = Args{
		Cmd:                  "inspect",
		OutputFormat:         "'{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'",
		AdditionalSuffixArgs: []string{"virtualbmc"},
		AuditWriter:          v.AuditWriter,
	}
	out, err := RunCommand(context.Background(), args)
	if err != nil {
		return netip.Addr{}, err
	}

	o := strings.Trim(strings.Trim(string(out), "\n"), "'")
	ip, err := netip.ParseAddr(o)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("error parsing Virtual BMC IP: %s: out: %v", err, string(out))
	}

	return ip, nil
}

func (v VirtualBMC) RegisterVirtualBMC(ctx context.Context) error {
	/*
		docker exec virtualbmc vbmc add --username admin --password password --port 623 node1
	*/
	for _, bmc := range v.BMCInfo {
		args := Args{
			Cmd: "exec",
			AdditionalPrefixArgs: []string{
				v.ContainerName,
				"vbmc", "add",
				"--username", bmc.Username,
				"--password", bmc.Password,
				"--port", bmc.Port,
				bmc.Hostname,
			},
			AuditWriter: v.AuditWriter,
		}
		if _, err := RunCommand(ctx, args); err != nil {
			return err
		}
	}

	return nil
}

func (v VirtualBMC) StartVirtualBMC(ctx context.Context) error {
	/*
		docker exec virtualbmc vbmc start node1
	*/
	for _, bmc := range v.BMCInfo {
		args := Args{
			Cmd:                  "exec",
			AdditionalPrefixArgs: []string{v.ContainerName, "vbmc", "start", bmc.Hostname},
			AuditWriter:          v.AuditWriter,
		}
		if _, err := RunCommand(ctx, args); err != nil {
			return err
		}
	}

	return nil
}
