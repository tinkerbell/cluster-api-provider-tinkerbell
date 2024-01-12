package internal

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"

	rufio "github.com/tinkerbell/rufio/api/v1alpha1"
	"github.com/tinkerbell/tink/api/v1alpha1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeData struct {
	// Hostname of the node
	Hostname string
	// IP address of the node
	IP netip.Addr
	// Netmask for the node
	Netmask net.IPMask
	// Gateway for the node to use
	Gateway netip.Addr
	// MAC address of the node
	MACAddress net.HardwareAddr
	// Nameservers for the node to use
	Nameservers []string
	// Disk is the disk device name for the node
	Disk string
	// Labels is a map of Labels to add to the node
	Labels map[string]string
	// BMCIP is the IP address of the BMC for the node
	BMCIP netip.AddrPort
	// BMCUsername is the username to use to connect to the BMC for the node
	BMCUsername string
	// BMCPassword is the password to use to connect to the BMC for the node
	BMCPassword string
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

func (d NodeData) Hardware(namespace string) v1alpha1.Hardware {
	return v1alpha1.Hardware{
		TypeMeta: v1.TypeMeta{
			Kind:       "Hardware",
			APIVersion: "tinkerbell.org/v1alpha1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      d.Hostname,
			Namespace: namespace,
			Labels:    d.Labels,
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
						MAC:         d.MACAddress.String(),
						Hostname:    d.Hostname,
						LeaseTime:   4294967294,
						NameServers: d.Nameservers,
						Arch:        "x86_64",
						IP: &v1alpha1.IP{
							Address: d.IP.String(),
							Netmask: net.IP(d.Netmask).String(),
							Gateway: d.Gateway.String(),
						},
					},
				},
			},
			Metadata: &v1alpha1.HardwareMetadata{
				Instance: &v1alpha1.MetadataInstance{
					Hostname: d.Hostname,
					ID:       d.MACAddress.String(),
				},
			},
			Disks: []v1alpha1.Disk{
				{Device: d.Disk},
			},
		},
	}
}

func (d NodeData) BMCMachine(namespace string) rufio.Machine {
	return rufio.Machine{
		TypeMeta: v1.TypeMeta{
			Kind:       "Machine",
			APIVersion: "bmc.tinkerbell.org/v1alpha1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("bmc-%s", d.Hostname),
			Namespace: namespace,
		},
		Spec: rufio.MachineSpec{
			Connection: rufio.Connection{
				AuthSecretRef: corev1.SecretReference{
					Name:      fmt.Sprintf("bmc-%s-creds", d.Hostname),
					Namespace: namespace,
				},
				Host:        d.BMCIP.Addr().String(),
				InsecureTLS: true,
				ProviderOptions: &rufio.ProviderOptions{
					IPMITOOL: &rufio.IPMITOOLOptions{
						Port: int(d.BMCIP.Port()),
					},
				},
			},
		},
	}
}

func (d NodeData) BMCSecret(namespace string) corev1.Secret {
	return corev1.Secret{
		TypeMeta: v1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("bmc-%s-creds", d.Hostname),
			Namespace: namespace,
		},
		Type: "kubernetes.io/basic-auth",
		Data: map[string][]byte{
			"username": []byte(d.BMCUsername),
			"password": []byte(d.BMCPassword),
		},
	}
}

func MarshalOrEmpty(h any) []byte {
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
