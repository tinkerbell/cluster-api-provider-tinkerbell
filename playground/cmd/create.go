package cmd

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v3"
	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/capi"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/docker"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/helm"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/kind"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/kubectl"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/libvirt"
	"github.com/tinkerbell/cluster-api-provider/playground/internal/tinkerbell"
	"gopkg.in/yaml.v3"
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
	// CAPTVersion is the version of CAPT that will be used to create the workload cluster
	CAPTVersion string
	// CAPTImageTag is the image tag of CAPT that will be used to create the workload cluster
	CAPTImageTag string
	// OSImage is the location to an image that lives in an OCI registry
	// Image should have the format: {{.OSRegistry}}/{{.OSDistro}}-{{.OSVersion}}:{{.KubernetesVersion}}.gz
	// example: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu-2004:v1.23.5.gz
	OSImage    string
	nodeData   []tinkerbell.NodeData
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
		ShortUsage: "capt-playground create [flags]",
		ShortHelp:  "Create the CAPT playground",
		Options:    []ff.Option{ff.WithEnvVarPrefix("CAPT_PLAYGROUND")},
		FlagSet:    fs,
		Exec: func(ctx context.Context, _ []string) error {
			return c.exec(ctx)
		},
	}
}

func (c *Create) registerFlags(fs *flag.FlagSet) {
	homedir := ""
	if h, err := os.UserHomeDir(); err == nil {
		homedir = h
	}
	fs.StringVar(&c.ClusterName, "name", ClusterName, "name of the cluster")
	fs.StringVar(&c.OutputDir, "output-dir", "./output", "directory location for all created files")
	fs.IntVar(&c.TotalHardware, "total-hardware", 4, "number of hardware CR that will be created in the management cluster")
	fs.IntVar(&c.ControlPlaneNodes, "control-plane-nodes", 1, "number of control plane nodes that will be created in the workload cluster")
	fs.IntVar(&c.WorkerNodes, "worker-nodes", 2, "number of worker nodes that will be created in the workload cluster")
	fs.StringVar(&c.KubernetesVersion, "kubernetes-version", "v1.23.5", "version of Kubernetes that will be used to create the workload cluster")
	fs.StringVar(&c.Namespace, "namespace", "tink-system", "namespace to use for all Objects created")
	fs.StringVar(&c.TinkerbellStackVersion, "tinkerbell-stack-version", "0.4.3", "version of the Tinkerbell stack that will be deployed to the management cluster")
	fs.StringVar(&c.SSHPublicKeyFile, "ssh-public-key-file", filepath.Join(homedir, ".ssh", "id_rsa.pub"), "file location of the SSH public key that will be added to all control plane and worker nodes in the workload cluster")
	fs.StringVar(&c.CAPTVersion, "capt-version", "0.5.0", "version of CAPT that will be installed in the management cluster")
	fs.StringVar(&c.CAPTImageTag, "capt-image-tag", "v0.5.0", "container image tag of CAPT manager that will be deployed to the management cluster")
	fs.StringVar(&c.OSImage, "os-image", "", "base OCI registry where OS image(s) lives (default: ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu-2004:v1.23.5.gz) ")
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

	st := struct {
		ClusterName   string `yaml:"clusterName"`
		OutputDir     string `yaml:"outputDir"`
		TotalHardware int    `yaml:"totalHardware"`
	}{
		ClusterName:   c.ClusterName,
		OutputDir:     c.OutputDir,
		TotalHardware: c.TotalHardware,
	}

	d, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if err := os.WriteFile(c.rootConfig.StateFile, d, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	p := filepath.Join(c.OutputDir, "apply")
	if err := os.MkdirAll(p, 0755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("error creating output dir: %w", err)
	}
	auditWriter, err := os.OpenFile(filepath.Join(c.OutputDir, "output.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return fmt.Errorf("error opening audit log: %w", err)
	}
	defer auditWriter.Close()
	outputWriter, err := os.OpenFile(filepath.Join(c.OutputDir, "output.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return fmt.Errorf("error opening output log: %w", err)
	}
	defer outputWriter.Close()
	// We need the docker network created first so that other containers and VMs can connect to it.
	log.Println("Creating KinD cluster")
	if err := kind.CreateCluster(ctx, kind.Args{Name: "playground", Kubeconfig: c.kubeconfig, AuditWriter: auditWriter}); err != nil {
		return fmt.Errorf("error creating kind cluster: %w", err)
	}

	log.Println("Starting Virtual BMC server")
	// This runs before creating the data slice so that we can get the IP of the Virtual BMC container.
	vbmc := docker.VirtualBMC{
		Network:       "kind",
		ContainerName: "virtualbmc",
		LibvirtSocket: "/var/run/libvirt/libvirt-sock",
		Image:         "ghcr.io/jacobweinstock/virtualbmc",
		AuditWriter:   auditWriter,
	}
	vbmcIP, err := vbmc.RunVirtualBMCContainer(context.Background())
	if err != nil {
		return fmt.Errorf("error starting Virtual BMC server: %s", err)
	}

	// get the gateway of the kind network
	do := docker.Opts{AuditWriter: auditWriter}
	gateway, err := do.IPv4GatewayFrom("kind")
	if err != nil {
		return fmt.Errorf("error getting gateway: %s", err)
	}

	subnet, err := do.IPv4SubnetFrom("kind")
	if err != nil {
		return fmt.Errorf("error getting subnet: %s", err)
	}

	log.Println("Populating node data")
	c.nodeData = c.populateNodeData(vbmcIP, subnet, gateway)

	log.Println("Deploying Tinkerbell stack")
	base := fmt.Sprintf("%v.%v.100", vbmcIP.As4()[0], vbmcIP.As4()[1]) // x.x.100
	tinkerbellVIP := fmt.Sprintf("%v.%d", base, 101)                   // x.x.100.101
	if err := c.deployTinkerbellStack(tinkerbellVIP, auditWriter, outputWriter); err != nil {
		return fmt.Errorf("error deploying Tinkerbell stack: %s", err)
	}

	log.Println("Creating Tinkerbell Custom Resource manifests")
	if err := writeYamls(c.nodeData, p, c.Namespace); err != nil {
		return fmt.Errorf("error writing yamls: %s", err)
	}

	log.Println("Create VMs")
	bridge, err := do.LinuxBridgeFrom("kind")
	if err != nil {
		return fmt.Errorf("error during VM creation: %w", err)
	}
	for _, d := range c.nodeData {
		d := d
		lo := libvirt.Opts{AuditWriter: auditWriter}
		if err := lo.CreateVM(d.Hostname, bridge, d.MACAddress); err != nil {
			return fmt.Errorf("error during VM creation: %w", err)
		}
	}

	log.Println("Starting Virtual BMCs")
	for _, d := range c.nodeData {
		n := docker.BMCInfo{
			Username: d.BMCUsername,
			Password: d.BMCPassword,
			Hostname: d.Hostname,
			Port:     fmt.Sprintf("%d", d.BMCIP.Port()),
		}
		vbmc.BMCInfo = append(vbmc.BMCInfo, n)
		vbmc.AuditWriter = auditWriter
	}
	if err := vbmc.RegisterVirtualBMC(context.Background()); err != nil {
		return fmt.Errorf("error starting Virtual BMCs: %s", err)
	}
	if err := vbmc.StartVirtualBMC(context.Background()); err != nil {
		return fmt.Errorf("error starting Virtual BMCs: %s", err)
	}

	log.Println("Apply Tinkerbell Custom Resource manifests")
	ko := kubectl.Opts{
		Kubeconfig:  c.kubeconfig,
		CacheDir:    c.OutputDir,
		AuditWriter: auditWriter,
	}
	if err := ko.ApplyFiles(context.Background(), []string{filepath.Join(c.OutputDir, "apply") + "/"}); err != nil {
		return fmt.Errorf("error applying Tinkerbell manifests: %w", err)
	}

	log.Println("Creating clusterctl.yaml")
	if err := capi.ClusterctlYamlToDisk(c.OutputDir, c.CAPTVersion, c.CAPTImageTag); err != nil {
		return fmt.Errorf("error creating clusterctl.yaml: %w", err)
	}

	capiOpts := capi.Opts{AuditWriter: auditWriter}
	log.Println("Running clusterctl init")
	if _, err := capi.ClusterctlInit(c.OutputDir, c.kubeconfig, tinkerbellVIP, capiOpts); err != nil {
		return fmt.Errorf("error running clusterctl init: %w", err)
	}

	log.Println("Running clusterctl generate cluster")
	podCIDR := fmt.Sprintf("%v.100.0.0/16", vbmcIP.As4()[0]) // x.100.0.0/16 (172.25.0.0/16)
	controlPlaneVIP := fmt.Sprintf("%v.%d", base, 100)       // x.x.100.100
	if err := capi.ClusterYamlToDisk(c.OutputDir, c.ClusterName, c.Namespace, strconv.Itoa(c.ControlPlaneNodes), strconv.Itoa(c.WorkerNodes), c.KubernetesVersion, controlPlaneVIP, podCIDR, c.kubeconfig, capiOpts); err != nil {
		return fmt.Errorf("error running clusterctl generate cluster: %w", err)
	}

	img := kubectl.OSImage{}
	if c.OSImage != "" {
		// Parse OSImage to get the registry, distro, and version
		registry, distro, distroVer, kubeVer, err := parseOSImage(c.OSImage)
		if err != nil {
			return fmt.Errorf("error parsing OS image from -os-image: %w", err)
		}
		img.Registry = registry
		img.Distro = distro
		img.Version = distroVer

		if kubeVer != c.KubernetesVersion {
			return fmt.Errorf("kubernetes version %s does not match the version in the OS image %s", c.KubernetesVersion, kubeVer)
		}
	}
	if err := ko.KustomizeClusterYaml(c.OutputDir, c.ClusterName, c.SSHPublicKeyFile, capi.KustomizeYaml, c.Namespace, string(CAPTRole), img); err != nil {
		return fmt.Errorf("error running kustomize: %w", err)
	}

	return nil
}

func parseOSImage(i string) (registry string, distro string, distroVer string, kubeVer string, err error) {
	// expected format is: {{.OSRegistry}}/{{.OSDistro}}-{{.OSVersion}}:{{.KubernetesVersion}}.gz
	// ghcr.io/tinkerbell/cluster-api-provider-tinkerbell/ubuntu-2004:v1.23.5.gz
	// registry: ghcr.io
	// distro: ubuntu
	// version: 2004
	// k8s version: v1.23.5

	registry = filepath.Dir(i)
	if registry == "" {
		return "", "", "", "", fmt.Errorf("unable to parse registry from %s", i)
	}
	imageAndTag := filepath.Base(i)
	image := strings.Split(imageAndTag, ":")[0]
	if image == "" {
		return "", "", "", "", fmt.Errorf("unable to parse image from %s", i)
	}
	tag := strings.Split(imageAndTag, ":")[1]
	if tag == "" {
		return "", "", "", "", fmt.Errorf("unable to parse tag from %s", i)
	}
	distro = strings.Split(image, "-")[0]
	if distro == "" {
		return "", "", "", "", fmt.Errorf("unable to parse distro from %s", i)
	}
	distroVer = strings.Split(image, "-")[1]
	if distroVer == "" {
		return "", "", "", "", fmt.Errorf("unable to parse distro version from %s", i)
	}
	kubeVer = strings.Split(tag, ".gz")[0]
	if kubeVer == "" {
		return "", "", "", "", fmt.Errorf("unable to parse kubernetes version from %s", i)
	}

	return registry, distro, distroVer, kubeVer, nil
}

func (c *Create) populateNodeData(vbmcIP netip.Addr, subnet net.IPMask, gateway netip.Addr) []tinkerbell.NodeData {
	// Use the vbmcIP in order to determine the subnet for the KinD network.
	// This is used to create the IP addresses for the VMs, Tinkerbell stack LB IP, and the KubeAPI server VIP.
	base := fmt.Sprintf("%v.%v.100", vbmcIP.As4()[0], vbmcIP.As4()[1]) // x.x.100
	nd := make([]tinkerbell.NodeData, c.TotalHardware)
	curControlPlaneNodesCount := 0
	curWorkerNodesCount := 0
	for i := 0; i < c.TotalHardware; i++ {
		num := i + 1
		d := tinkerbell.NodeData{
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

func (c *Create) deployTinkerbellStack(tinkVIP string, auditWriter, outputWriter io.Writer) error {
	/*
		trusted_proxies=$(kubectl get nodes -o jsonpath='{.items[*].spec.podCIDR}')
		LB_IP=x.x.x.x
		helm install tink-stack oci://ghcr.io/tinkerbell/charts/stack --version "$STACK_CHART_VERSION" --create-namespace --namespace tink-system --wait --set "smee.trustedProxies={${trusted_proxies}}" --set "hegel.trustedProxies={${trusted_proxies}}" --set "stack.loadBalancerIP=$LB_IP" --set "smee.publicIP=$LB_IP"
	*/
	var trustedProxies []string
	timeout := time.NewTimer(time.Minute)
	auditOnce := true
LOOP:
	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("unable to get node cidrs after 1 minute")
		default:
		}
		ko := kubectl.Opts{
			Kubeconfig: c.kubeconfig,
			CacheDir:   c.OutputDir,
		}
		if auditOnce {
			ko.AuditWriter = auditWriter
			auditOnce = false
		}

		cidrs, err := ko.GetNodeCidrs(context.Background())
		if err != nil {
			return fmt.Errorf("error getting node cidrs: %w", err)
		}
		for _, c := range cidrs {
			v, cdr, _ := net.ParseCIDR(c)
			if v != nil {
				trustedProxies = append(trustedProxies, cdr.String())
				break LOOP
			}
		}
	}

	a := helm.Args{
		ReleaseName: "tink-stack",
		Chart: &url.URL{
			Scheme: "oci",
			Host:   "ghcr.io",
			Path:   "/tinkerbell/charts/stack",
		},
		Version:         c.TinkerbellStackVersion,
		CreateNamespace: true,
		Namespace:       c.Namespace,
		Wait:            true,
		SetArgs: map[string]string{
			"smee.trustedProxies":  fmt.Sprintf("{%s}", strings.Join(trustedProxies, ",")),
			"hegel.trustedProxies": fmt.Sprintf("{%s}", strings.Join(trustedProxies, ",")),
			"stack.loadBalancerIP": tinkVIP,
			"smee.publicIP":        tinkVIP,
		},
		Kubeconfig:  c.kubeconfig,
		CacheDir:    c.OutputDir,
		AuditWriter: auditWriter,
	}
	if err := helm.Install(context.Background(), a); err != nil {
		return fmt.Errorf("error deploying Tinkerbell stack: %w", err)
	}

	return nil
}

func writeYamls(ds []tinkerbell.NodeData, outputDir string, namespace string) error {
	for _, d := range ds {
		y := []struct {
			name string
			data []byte
		}{
			{name: fmt.Sprintf("hardware-%s.yaml", d.Hostname), data: tinkerbell.MarshalOrEmpty(d.Hardware(namespace))},
			{name: fmt.Sprintf("bmc-machine-%s.yaml", d.Hostname), data: tinkerbell.MarshalOrEmpty(d.BMCMachine(namespace))},
			{name: fmt.Sprintf("bmc-secret-%s.yaml", d.Hostname), data: tinkerbell.MarshalOrEmpty(d.BMCSecret(namespace))},
		}

		for _, yaml := range y {
			if err := os.WriteFile(filepath.Join(outputDir, yaml.name), yaml.data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
