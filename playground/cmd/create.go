package cmd

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/tinkerbell/cluster-api-provider/playground/internal"
)

type Create struct {
	// ClusterName of the cluster
	ClusterName string
	// OutputDir is the directory where all created files will be saved
	OutputDir string
	// TotalHardware is the number of hardware CR that will be created in the management cluster
	TotalHardware int
	// ControlPlaneNodes is the number of control plane nodes that will be created in the workload cluster
	ControlPlaneNodes int
	// WorkerNodes is the number of worker nodes that will be created in the workload cluster
	WorkerNodes int
	// KubernetesVersion is the version of Kubernetes that will be used to create the workload cluster
	KubernetesVersion string
	// Namespace to use for all Objects created
	Namespace string
	// TinkerbellStackVersion is the version of the Tinkerbell stack that will be deployed to the management cluster
	TinkerbellStackVersion string
	// SSHPublicKeyFile is the file location of the SSH public key that will be added to all control plane and worker nodes in the workload cluster
	SSHPublicKeyFile string
	// nodeData holds data for each node that will be created
	nodeData   []internal.NodeData
	rootConfig *rootConfig
	kubeconfig string
}

func NewCreateCommand(rc *rootConfig) *ffcli.Command {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	c := &Create{rootConfig: rc}
	c.registerFlags(fs)
	rc.registerRootFlags(fs)
	return &ffcli.Command{
		Name:       "create",
		ShortUsage: "create the CAPT playground [flags]",
		Options:    []ff.Option{ff.WithEnvVarPrefix("CAPT_PLAYGROUND")},
		FlagSet:    fs,
		Exec: func(context.Context, []string) error {
			println("create")
			fmt.Printf("create: %+v\n", c.rootConfig)

			return nil
		},
	}
}

func (c *Create) registerFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.ClusterName, "name", ClusterName, "name of the cluster")
	fs.StringVar(&c.OutputDir, "output-dir", "./output", "directory location for all created files")
	fs.IntVar(&c.TotalHardware, "total-hardware", 4, "number of hardware CR that will be created in the management cluster")
	fs.IntVar(&c.ControlPlaneNodes, "control-plane-nodes", 1, "number of control plane nodes that will be created in the workload cluster")
	fs.IntVar(&c.WorkerNodes, "worker-nodes", 2, "number of worker nodes that will be created in the workload cluster")
	fs.StringVar(&c.KubernetesVersion, "kubernetes-version", "v1.20.5", "version of Kubernetes that will be used to create the workload cluster")
	fs.StringVar(&c.Namespace, "namespace", "capt-playground", "namespace to use for all Objects created")
	fs.StringVar(&c.TinkerbellStackVersion, "tinkerbell-stack-version", "v0.5.0", "version of the Tinkerbell stack that will be deployed to the management cluster")
	fs.StringVar(&c.SSHPublicKeyFile, "ssh-public-key-file", "", "file location of the SSH public key that will be added to all control plane and worker nodes in the workload cluster")
}

func (c *Create) exec(ctx context.Context) error {
	// create kind cluster
	// create output dir
	// create virtualbmc docker container
	// create all virsh nodes
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "./"
	}
	c.kubeconfig = filepath.Join(pwd, c.OutputDir, "kind.kubeconfig")
	// We need the docker network created first so that other containers and VMs can connect to it.
	log.Println("create kind cluster")
	if err := c.createKindCluster(); err != nil {
		return fmt.Errorf("error creating kind cluster: %w", err)
	}

	// This runs before creating the data slice so that we can get the IP of the Virtual BMC container.
	log.Println("Start Virtual BMC")
	vbmcIP, err := startVirtualBMC("kind")
	if err != nil {
		log.Fatalf("error starting Virtual BMC: %s", err)
	}

	// get the gateway of the kind network
	gateway, err := getGateway("kind")
	if err != nil {
		log.Fatalf("error getting gateway: %s", err)
	}

	subnet, err := getSubnet("kind")
	if err != nil {
		log.Fatalf("error getting subnet: %s", err)
	}

	log.Println("Populating node data")
	c.nodeData = c.populateNodeData(vbmcIP, subnet, gateway)

	log.Println("deploy Tinkerbell stack")
	base := fmt.Sprintf("%v.%v.100", vbmcIP.As4()[0], vbmcIP.As4()[1]) // x.x.100
	tinkerbellVIP := fmt.Sprintf("%v.%d", base, 101)                   // x.x.100.101
	if err := c.deployTinkerbellStack(tinkerbellVIP, c.Namespace); err != nil {
		log.Fatalf("error deploying Tinkerbell stack: %s", err)
	}

	log.Println("creating Tinkerbell Custom Resources")
	if err := writeYamls(c.nodeData, c.OutputDir, c.Namespace); err != nil {
		log.Fatalf("error writing yamls: %s", err)
	}

	return nil
}

func (c *Create) createKindCluster() error {
	/*
		kind create cluster --name <clusterName> --kubeconfig <outputDir>/kind.kubeconfig
	*/
	cmd := "kind"
	args := []string{"create", "cluster", "--name", c.ClusterName, "--kubeconfig", c.kubeconfig}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error creating kind cluster: %s: out: %v", err, string(out))
	}

	return nil
}

func startVirtualBMC(dockerNet string) (netip.Addr, error) {
	/*
		docker run -d --rm --network kind -v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock --name virtualbmc capt-playground:v2
	*/
	cmd := "docker"
	args := []string{
		"run", "-d", "--rm",
		"--network", dockerNet,
		"-v", "/var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro",
		"-v", "/var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock",
		"--name", "virtualbmc",
		"capt-playground:v2",
	}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("error starting Virtual BMC: %s: out: %v", err, string(out))
	}

	// get the IP of the container
	args = []string{
		"inspect", "-f", "'{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'", "virtualbmc",
	}
	e = exec.CommandContext(context.Background(), cmd, args...)
	out, err = e.CombinedOutput()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("error getting Virtual BMC IP: %s: out: %v", err, string(out))
	}

	o := strings.Trim(strings.Trim(string(out), "\n"), "'")
	ip, err := netip.ParseAddr(o)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("error parsing Virtual BMC IP: %s: out: %v", err, string(out))
	}

	return ip, nil
}

func getGateway(dockerNet string) (netip.Addr, error) {
	/*
		docker network inspect kind -f '{{range .IPAM.Config}}{{.Gateway}},{{end}}'
		result: 172.20.0.1,
	*/
	cmd := "docker"
	args := []string{"network", "inspect", dockerNet, "-f", "'{{range .IPAM.Config}}{{.Gateway}},{{end}}'"}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("error getting gateway: %s: out: %v", err, string(out))
	}

	o := strings.Trim(strings.Trim(string(out), "\n"), "'")
	subnets := strings.Split(o, ",")
	for _, s := range subnets {
		ip, err := netip.ParseAddr(s)
		if err == nil && ip.Is4() {
			return ip, nil
		}
	}

	return netip.Addr{}, fmt.Errorf("unable to determine docker network gateway, err from command: %s: stdout: %v", err, string(out))
}

func getSubnet(dockerNet string) (net.IPMask, error) {
	/*
		docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}},{{end}}'
		result: 172.20.0.0/16,fc00:f853:ccd:e793::/64,
	*/
	cmd := "docker"
	args := []string{"network", "inspect", dockerNet, "-f", "'{{range .IPAM.Config}}{{.Subnet}},{{end}}'"}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("error getting subnet: %s: out: %v", err, string(out))
	}

	o := strings.Trim(strings.Trim(string(out), "\n"), "'")
	subnets := strings.Split(o, ",")
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

func (c *Create) populateNodeData(vbmcIP netip.Addr, subnet net.IPMask, gateway netip.Addr) []internal.NodeData {
	// Use the vbmcIP in order to determine the subnet for the KinD network.
	// This is used to create the IP addresses for the VMs, Tinkerbell stack LB IP, and the KubeAPI server VIP.
	base := fmt.Sprintf("%v.%v.100", vbmcIP.As4()[0], vbmcIP.As4()[1]) // x.x.100
	nd := make([]internal.NodeData, c.TotalHardware)
	curControlPlaneNodesCount := 0
	curWorkerNodesCount := 0
	for i := 0; i < c.TotalHardware; i++ {
		num := i + 1
		d := internal.NodeData{
			Hostname:    fmt.Sprintf("node%v", num),
			MACAddress:  net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			Nameservers: []string{"8.8.8.8", "1.1.1.1"},
			IP:          netip.MustParseAddr(fmt.Sprintf("%v.%d", base, num)),
			Netmask:     subnet,
			Gateway:     gateway,
			Disk:        "/dev/vda",
			BMCIP:       netip.AddrPortFrom(vbmcIP, uint16(6230+num)),
			BMCUsername: "admin",
			BMCPassword: "password",
			Labels:      map[string]string{},
		}
		if m, err := GenerateRandMAC(); err == nil {
			d.MACAddress = m
		}
		if curControlPlaneNodesCount < c.ControlPlaneNodes {
			d.Labels[string(CAPTRole)] = string(ControlPlaneRole)
			curControlPlaneNodesCount++
		} else if curWorkerNodesCount < c.WorkerNodes {
			d.Labels[string(CAPTRole)] = string(WorkerRole)
			curWorkerNodesCount++
		}
		nd[i] = d
	}

	return nd
}

// GenerateRandMAC generates a random MAC address.
func GenerateRandMAC() (net.HardwareAddr, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("unable to retrieve 6 rnd bytes: %s", err)
	}

	// Set locally administered addresses bit and reset multicast bit
	buf[0] = (buf[0] | 0x02) & 0xfe

	return buf, nil
}

func (c *Create) deployTinkerbellStack(tinkVIP string, namespace string) error {
	/*
		trusted_proxies=$(kubectl get nodes -o jsonpath='{.items[*].spec.podCIDR}')
		LB_IP=x.x.x.x
		helm install tink-stack oci://ghcr.io/tinkerbell/charts/stack --version "$STACK_CHART_VERSION" --create-namespace --namespace tink-system --wait --set "smee.trustedProxies={${trusted_proxies}}" --set "hegel.trustedProxies={${trusted_proxies}}" --set "stack.loadBalancerIP=$LB_IP" --set "smee.publicIP=$LB_IP"
	*/
	var trustedProxies string
	for {
		cmd := "kubectl"
		args := []string{"get", "nodes", "-o", "jsonpath='{.items[*].spec.podCIDR}'"}
		e := exec.CommandContext(context.Background(), cmd, args...)
		e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", c.kubeconfig)}
		out, err := e.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error getting trusted proxies: %s: out: %v", err, string(out))
		}
		// strip quotes
		trustedProxies = strings.Trim(string(out), "'")
		v, _, _ := net.ParseCIDR(trustedProxies)
		if v != nil {
			break
		}
	}

	cmd := "helm"
	args := []string{
		"install", "tink-stack", "oci://ghcr.io/tinkerbell/charts/stack",
		"--version", c.TinkerbellStackVersion,
		"--create-namespace", "--namespace", namespace,
		"--wait",
		"--set", fmt.Sprintf("smee.trustedProxies={%s}", trustedProxies),
		"--set", fmt.Sprintf("hegel.trustedProxies={%s}", trustedProxies),
		"--set", fmt.Sprintf("stack.loadBalancerIP=%s", tinkVIP),
		"--set", fmt.Sprintf("smee.publicIP=%s", tinkVIP),
		"--set", "rufio.image=quay.io/tinkerbell/rufio:latest",
	}
	e := exec.CommandContext(context.Background(), cmd, args...)
	e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", c.kubeconfig)}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error deploying Tinkerbell stack: %s: out: %v", err, string(out))
	}

	return nil
}

func writeYamls(ds []internal.NodeData, outputDir string, namespace string) error {
	p := filepath.Join(outputDir, "apply")
	if err := os.MkdirAll(p, 0755); err != nil && !os.IsExist(err) {
		return err
	}
	for _, d := range ds {
		y := []struct {
			name string
			data []byte
		}{
			{name: fmt.Sprintf("hardware-%s.yaml", d.Hostname), data: internal.MarshalOrEmpty(d.Hardware(namespace))},
			{name: fmt.Sprintf("bmc-machine-%s.yaml", d.Hostname), data: internal.MarshalOrEmpty(d.BMCMachine(namespace))},
			{name: fmt.Sprintf("bmc-secret-%s.yaml", d.Hostname), data: internal.MarshalOrEmpty(d.BMCSecret(namespace))},
		}

		for _, yaml := range y {
			if err := os.WriteFile(filepath.Join(p, yaml.name), yaml.data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
