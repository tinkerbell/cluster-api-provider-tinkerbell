package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	rufio "github.com/tinkerbell/rufio/api/v1alpha1"
	"github.com/tinkerbell/tink/api/v1alpha1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	controlPlaneNodeRole nodeRole  = "control-plane"
	workerNodeRole       nodeRole  = "worker"
	captRoleLabel        captLabel = "capt-node-role"
	clusterName                    = "playground"
	controlPlaneVIP                = "172.18.18.17"
	tinkerbellVIP                  = "172.18.18.18"
)

type captLabel string

type nodeRole string

type ymls []yml

type yml struct {
	data []byte
	name string
}

type cluster struct {
	hardwareCount          int
	controlPlaneNodesCount int
	workerNodesCount       int
	kubernetesVersion      string
	namespace              string
	outputDir              string
	kubeconfig             string
	tinkerbellStackVer     string
	data                   []data
}

type data struct {
	Hostname    string
	Namespace   string
	Mac         net.HardwareAddr
	Nameservers []string
	IP          netip.Addr
	Netmask     netip.Addr
	Gateway     netip.Addr
	Disk        string
	BMCHostname string
	BMCIPPort   netip.AddrPort
	BMCUsername string
	BMCPassword string
	labels      map[string]string
}

func main() {
	fs := flag.NewFlagSet("capt-playground", flag.ExitOnError)
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "./"
	}
	c := cluster{
		kubeconfig: filepath.Join(pwd, "output/kind.kubeconfig"),
	}
	fs.IntVar(&c.hardwareCount, "hardware-count", 4, "number of hardware to create")
	fs.IntVar(&c.controlPlaneNodesCount, "control-plane-nodes-count", 1, "number of control plane nodes to create")
	fs.IntVar(&c.workerNodesCount, "worker-nodes-count", 2, "number of worker nodes to create")
	fs.StringVar(&c.namespace, "namespace", "tink-system", "namespace for all resources")
	fs.StringVar(&c.kubernetesVersion, "kubernetes-version", "v1.23.5", "kubernetes version to install")
	fs.StringVar(&c.outputDir, "output-dir", "output", "directory to all produced artifacts (yamls, kubeconfig, etc)")
	fs.StringVar(&c.tinkerbellStackVer, "tinkerbell-stack-version", "0.4.2", "tinkerbell stack version to install")
	fs.Parse(os.Args[1:])

	c.data = make([]data, c.hardwareCount)
	curControlPlaneNodesCount := 0
	curWorkerNodesCount := 0
	for i := 0; i < c.hardwareCount; i++ {
		num := i + 1
		d := data{
			Hostname:    fmt.Sprintf("node%v", num),
			Namespace:   c.namespace,
			Mac:         net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			Nameservers: []string{"8.8.8.8", "1.1.1.1"},
			IP:          netip.MustParseAddr(fmt.Sprintf("172.18.19.%d", num)),
			Netmask:     netip.MustParseAddr("255.255.0.0"),
			Gateway:     netip.MustParseAddr("172.18.0.1"),
			Disk:        "/dev/vda",
			BMCHostname: "virtualbmc",
			BMCIPPort:   netip.MustParseAddrPort(fmt.Sprintf("0.0.0.0:623%v", num)),
			BMCUsername: "admin",
			BMCPassword: "password",
			labels:      map[string]string{},
		}
		if m, err := GenerateRandMAC(); err == nil {
			d.Mac = m
		}

		if curControlPlaneNodesCount < c.controlPlaneNodesCount {
			d.labels[captRoleLabel.String()] = controlPlaneNodeRole.String()
			curControlPlaneNodesCount++
		} else if curWorkerNodesCount < c.workerNodesCount {
			d.labels[captRoleLabel.String()] = workerNodeRole.String()
			curWorkerNodesCount++
		}
		c.data[i] = d
	}

	log.Println("create kind cluster")
	if err := c.createKindCluster(clusterName); err != nil {
		log.Fatalf("error creating kind cluster: %s", err)
	}

	log.Println("deploy Tinkerbell stack")
	if err := c.deployTinkerbellStack(); err != nil {
		log.Fatalf("error deploying Tinkerbell stack: %s", err)
	}

	log.Println("creating Tinkerbell Custom Resources")
	if err := writeYamls(c.data, c.outputDir); err != nil {
		log.Fatalf("error writing yamls: %s", err)
	}

	log.Println("creating clusterctl.yaml")
	if err := writeClusterctlYaml("output"); err != nil {
		log.Fatalf("error writing clusterctl.yaml: %s", err)
	}

	log.Println("running clusterctl init")
	if err := c.clusterctlInit(c.outputDir, tinkerbellVIP); err != nil {
		log.Fatalf("error running clusterctl init: %s", err)
	}

	log.Println("running clusterctl generate cluster")
	if err := c.clusterctlGenerateClusterYaml(c.outputDir, clusterName, c.namespace, c.controlPlaneNodesCount, c.workerNodesCount, c.kubernetesVersion, controlPlaneVIP); err != nil {
		log.Fatalf("error running clusterctl generate cluster: %s", err)
	}
	if err := c.kustomizeClusterYaml(c.outputDir); err != nil {
		log.Fatalf("error running kustomize: %s", err)
	}

	log.Println("getting KinD bridge")
	bridge, err := getKinDBridge("kind")
	if err != nil {
		log.Fatalf("error getting KinD bridge: %s", err)
	}
	log.Println("creating VMs")
	if err := createVMs(c.data, bridge); err != nil {
		log.Fatalf("error creating vms: %s\n", err)
	}

	log.Println("Start Virtual BMC")
	if err := startVirtualBMC("kind"); err != nil {
		log.Fatalf("error starting Virtual BMC: %s", err)
	}

	log.Println("Register and start Virtual BMCs for all nodes")
	if err := registerAndStartVirtualBMCs(c.data); err != nil {
		log.Fatalf("error registering and starting Virtual BMCs: %s", err)
	}
}

func registerAndStartVirtualBMCs(ds []data) error {
	/*
		for i in {1..4}; do echo $i; docker exec virtualbmc vbmc add --username admin --password password --port "623$i" --no-daemon "node$i"; done
		for i in {1..4}; do echo $i; docker exec virtualbmc vbmc start "node$i"; done
	*/
	cmd := "docker"
	for _, d := range ds {
		d := d
		args := []string{
			"exec", "virtualbmc",
			"vbmc", "add",
			"--username", d.BMCUsername,
			"--password", d.BMCPassword,
			"--port", fmt.Sprintf("%v", d.BMCIPPort.Port()),
			"--no-daemon", d.Hostname,
		}
		e := exec.CommandContext(context.Background(), cmd, args...)
		out, err := e.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error adding virtualbmc: %+v: error: %s: out: %v", d, err, string(out))
		}

		args = []string{
			"exec", "virtualbmc",
			"vbmc", "start",
			d.Hostname,
		}
		e = exec.CommandContext(context.Background(), cmd, args...)
		out, err = e.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error starting virtualbmc: %+v: error: %s: out: %v", d, err, string(out))
		}
	}

	return nil
}

func startVirtualBMC(dockerNet string) error {
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
		return fmt.Errorf("error starting Virtual BMC: %s: out: %v", err, string(out))
	}

	return nil
}

func (c cluster) createKindCluster(name string) error {
	/*
		kind create cluster --name playground --kubeconfig output/kind.kubeconfig
	*/
	cmd := "kind"
	args := []string{"create", "cluster", "--name", name, "--kubeconfig", filepath.Join(c.outputDir, "kind.kubeconfig")}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error creating kind cluster: %s: out: %v", err, string(out))
	}

	return nil
}

func (c cluster) deployTinkerbellStack() error {
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
		"--version", c.tinkerbellStackVer,
		"--create-namespace", "--namespace", c.namespace,
		"--wait",
		"--set", fmt.Sprintf("smee.trustedProxies={%s}", trustedProxies),
		"--set", fmt.Sprintf("hegel.trustedProxies={%s}", trustedProxies),
		"--set", fmt.Sprintf("stack.loadBalancerIP=%s", tinkerbellVIP),
		"--set", fmt.Sprintf("smee.publicIP=%s", tinkerbellVIP),
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

func (c cluster) clusterctlGenerateClusterYaml(outputDir string, clusterName string, namespace string, numCP int, numWorker int, k8sVer string, cpVIP string) error {
	/*
		CONTROL_PLANE_VIP=172.18.18.17 POD_CIDR=172.25.0.0/16 clusterctl generate cluster playground --config outputDir/clusterctl.yaml --kubernetes-version v1.23.5 --control-plane-machine-count=1 --worker-machine-count=2 --target-namespace=tink-system --write-to playground.yaml
	*/
	cmd := "clusterctl"
	args := []string{
		"generate", "cluster", clusterName,
		"--config", filepath.Join(outputDir, "clusterctl.yaml"),
		"--kubernetes-version", fmt.Sprintf("%v", k8sVer),
		fmt.Sprintf("--control-plane-machine-count=%v", numCP),
		fmt.Sprintf("--worker-machine-count=%v", numWorker),
		fmt.Sprintf("--target-namespace=%v", namespace),
		"--write-to", filepath.Join(outputDir, fmt.Sprintf("%v.yaml", clusterName)),
	}
	e := exec.CommandContext(context.Background(), cmd, args...)
	e.Env = []string{
		fmt.Sprintf("CONTROL_PLANE_VIP=%s", cpVIP),
		"POD_CIDR=172.25.0.0/16",
		fmt.Sprintf("KUBECONFIG=%s", c.kubeconfig),
		"XDG_CONFIG_HOME=/home/tink/xdg",
		"XDG_CONFIG_DIRS=/home/tink/xdg",
		"XDG_STATE_HOME=/home/tink/xdg",
		"XDG_CACHE_HOME=/home/tink/xdg",
		"XDG_RUNTIME_DIR=/home/tink/xdg",
		"XDG_DATA_HOME=/home/tink/xdg",
		"XDG_DATA_DIRS=/home/tink/xdg",
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running clusterctl generate cluster: %s: out: %v", err, string(out))
	}

	return nil
}

const kustomizeYaml = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: tink-system
resources:
  - playground.yaml
patches:
  - target:
      group: infrastructure.cluster.x-k8s.io
      kind: TinkerbellMachineTemplate
      name: ".*control-plane.*"
      version: v1beta1
    patch: |- 
      - op: add
        path: /spec/template/spec
        value: 
          hardwareAffinity:
            required:
            - labelSelector:
                matchLabels:
                  capt-node-role: control-plane
  - target:
      group: infrastructure.cluster.x-k8s.io
      kind: TinkerbellMachineTemplate
      name: ".*worker.*"
      version: v1beta1
    patch: |- 
      - op: add
        path: /spec/template/spec
        value: 
          hardwareAffinity:
            required:
            - labelSelector:
                matchLabels:
                  capt-node-role: worker`

func (c cluster) kustomizeClusterYaml(outputDir string) error {
	/*
		kubectl kustomize -o output/playground.yaml
	*/
	// write kustomization.yaml to output dir
	if err := os.WriteFile(filepath.Join(outputDir, "kustomization.yaml"), []byte(kustomizeYaml), 0644); err != nil {
		return err
	}
	cmd := "kubectl"
	args := []string{"kustomize", outputDir, "-o", filepath.Join(outputDir, "playground.yaml")}
	e := exec.CommandContext(context.Background(), cmd, args...)
	e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", c.kubeconfig)}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running kubectl kustomize: %s: out: %v", err, string(out))
	}

	return nil
}

func (c cluster) clusterctlInit(outputDir string, tinkVIP string) error {
	/*
		TINKERBELL_IP=172.18.18.18 clusterctl --config output/clusterctl.yaml init --infrastructure tinkerbell
	*/
	cmd := "clusterctl"
	args := []string{"init", "--config", filepath.Join(outputDir, "clusterctl.yaml"), "--infrastructure", "tinkerbell"}
	e := exec.CommandContext(context.Background(), cmd, args...)
	e.Env = []string{
		fmt.Sprintf("TINKERBELL_IP=%s", tinkVIP),
		fmt.Sprintf("KUBECONFIG=%s", c.kubeconfig),
		"XDG_CONFIG_HOME=/home/tink/xdg",
		"XDG_CONFIG_DIRS=/home/tink/xdg",
		"XDG_STATE_HOME=/home/tink/xdg",
		"XDG_CACHE_HOME=/home/tink/xdg",
		"XDG_RUNTIME_DIR=/home/tink/xdg",
		"XDG_DATA_HOME=/home/tink/xdg",
		"XDG_DATA_DIRS=/home/tink/xdg",
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running clusterctl init: %s: out: %v", err, string(out))
	}

	return nil
}

func writeClusterctlYaml(outputDir string) error {
	/*
			mkdir -p ~/.cluster-api
		    cat >> ~/.cluster-api/clusterctl.yaml <<EOF
		    providers:
		      - name: "tinkerbell"
		        url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/v0.4.0/infrastructure-components.yaml"
		        type: "InfrastructureProvider"
		    EOF
	*/

	contents := fmt.Sprintf(`providers:
  - name: "tinkerbell"
    url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/v%v/infrastructure-components.yaml"
    type: "InfrastructureProvider"`, "0.4.0")

	return os.WriteFile(filepath.Join(outputDir, "clusterctl.yaml"), []byte(contents), 0644)
}

func getKinDBridge(dockerNetName string) (string, error) {
	/*
			network_id=$(docker network inspect -f {{.Id}} kind)
		    bridge_name="br-${network_id:0:11}"
		    brctl show $bridge_name
	*/
	cmd := "docker"
	args := []string{"network", "inspect", "-f", "{{.Id}}", dockerNetName}
	e := exec.CommandContext(context.Background(), cmd, args...)
	out, err := e.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error getting bridge id: %s: out: %v", err, string(out))
	}
	bridgeID := string(out)[:12]
	bridgeName := fmt.Sprintf("br-%s", bridgeID)

	return bridgeName, nil
}

func createVMs(ds []data, bridgeName string) error {
	cmd := "virt-install"
	for _, d := range ds {
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
		d := d
		args = append(args, "--name", d.Hostname)
		args = append(args, "--disk", fmt.Sprintf("path=/tmp/%v-disk.img,bus=virtio,size=10,sparse=yes", d.Hostname))
		args = append(args, "--network", fmt.Sprintf("bridge:%s,mac=%s", bridgeName, d.Mac.String()))
		e := exec.CommandContext(context.Background(), cmd, args...)
		out, err := e.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error creating vm: %+v: error: %s: out: %v", d, err, string(out))
		}
	}
	return nil
}

func writeYamls(ds []data, outputDir string) error {
	p := filepath.Join(outputDir, "apply")
	if err := os.MkdirAll(p, 0755); err != nil && !os.IsExist(err) {
		return err
	}
	for _, d := range ds {
		y := createYamls(d)
		for _, yaml := range y {
			if err := os.WriteFile(filepath.Join(p, yaml.name), yaml.data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c captLabel) String() string {
	return string(c)
}

func (n nodeRole) String() string {
	return string(n)
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

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

func (d data) hardware() v1alpha1.Hardware {
	return v1alpha1.Hardware{
		TypeMeta: v1.TypeMeta{
			Kind:       "Hardware",
			APIVersion: "tinkerbell.org/v1alpha1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      d.Hostname,
			Namespace: d.Namespace,
			Labels:    d.labels,
		},
		Spec: v1alpha1.HardwareSpec{
			BMCRef: &corev1.TypedLocalObjectReference{
				APIGroup: stringPtr("bmc.tinkerbell.org"),
				Kind:     "Machine",
				Name:     fmt.Sprintf("bmc-%s", d.Hostname),
			},
			Interfaces: []v1alpha1.Interface{
				{
					Netboot: &v1alpha1.Netboot{
						AllowPXE:      boolPtr(true),
						AllowWorkflow: boolPtr(true),
					},
					DHCP: &v1alpha1.DHCP{
						MAC:         d.Mac.String(),
						Hostname:    d.Hostname,
						LeaseTime:   4294967294,
						NameServers: d.Nameservers,
						Arch:        "x86_64",
						IP: &v1alpha1.IP{
							Address: d.IP.String(),
							Netmask: d.Netmask.String(),
							Gateway: d.Gateway.String(),
						},
					},
				},
			},
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Hostname: d.Hostname,
					ID:       d.Mac.String(),
				},
			},
			Disks: []v1alpha1.Disk{
				{Device: d.Disk},
			},
		},
	}
}

func (d data) bmcMachine() rufio.Machine {
	return rufio.Machine{
		TypeMeta: v1.TypeMeta{
			Kind:       "Machine",
			APIVersion: "bmc.tinkerbell.org/v1alpha1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("bmc-%s", d.Hostname),
			Namespace: d.Namespace,
		},
		Spec: rufio.MachineSpec{
			Connection: rufio.Connection{
				AuthSecretRef: corev1.SecretReference{
					Name:      fmt.Sprintf("bmc-%s-creds", d.Hostname),
					Namespace: d.Namespace,
				},
				Host:        d.BMCHostname,
				InsecureTLS: true,
				ProviderOptions: &rufio.ProviderOptions{
					IPMITOOL: &rufio.IPMITOOLOptions{
						Port: int(d.BMCIPPort.Port()),
					},
				},
			},
		},
	}
}

func (d data) bmcSecret() corev1.Secret {
	return corev1.Secret{
		TypeMeta: v1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("bmc-%s-creds", d.Hostname),
			Namespace: d.Namespace,
		},
		Type: "kubernetes.io/basic-auth",
		Data: map[string][]byte{
			"username": []byte(d.BMCUsername),
			"password": []byte(d.BMCPassword),
		},
	}
}

func createYamls(r data) ymls {
	ymls := ymls{
		yml{
			name: fmt.Sprintf("hardware-%s.yaml", r.Hostname),
			data: marshal(r.hardware()),
		},
		yml{
			name: fmt.Sprintf("bmc-machine-%s.yaml", r.Hostname),
			data: marshal(r.bmcMachine()),
		},
		yml{
			name: fmt.Sprintf("bmc-secret-%s.yaml", r.Hostname),
			data: marshal(r.bmcSecret()),
		},
	}

	return ymls
}

func marshal(h any) []byte {
	b, err := Marshal(&h)
	if err != nil {
		return []byte{}
	}

	return b
}

// Marshal the object into JSON then convert
// JSON to YAML and returns the YAML.
func Marshal(o interface{}) ([]byte, error) {
	j, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("error marshaling into JSON: %v", err)
	}

	y, err := JSONToYAML(j)
	if err != nil {
		return nil, fmt.Errorf("error converting JSON to YAML: %v", err)
	}

	return y, nil
}

// JSONToYAML Converts JSON to YAML.
func JSONToYAML(j []byte) ([]byte, error) {
	// Convert the JSON to an object.
	var jsonObj interface{}
	// We are using yaml.Unmarshal here (instead of json.Unmarshal) because the
	// Go JSON library doesn't try to pick the right number type (int, float,
	// etc.) when unmarshalling to interface{}, it just picks float64
	// universally. go-yaml does go through the effort of picking the right
	// number type, so we can preserve number type throughout this process.
	err := yaml.Unmarshal(j, &jsonObj)
	if err != nil {
		return nil, err
	}

	// Marshal this object into YAML.
	return yaml.Marshal(jsonObj)
}
