/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type HardwareState string

const (
	HardwareError = HardwareState("Error")
	HardwareReady = HardwareState("Ready")
)

// HardwareSpec defines the desired state of Hardware.
type HardwareSpec struct {
	// ID is the ID of the hardware in Tinkerbell
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`
}

// HardwareStatus defines the observed state of Hardware.
type HardwareStatus struct {
	TinkMetadata string `json:"tinkMetadata,omitempty"`

	TinkVersion int64 `json:"tinkVersion,omitempty"`

	TinkInterfaces []Interface `json:"tinkInterfaces,omitempty"`

	State HardwareState `json:"state,omitempty"`
}

type Interface struct {
	//+optional
	Netboot *Netboot `json:"netboot,omitempty"`

	//+optional
	DHCP *DHCP `json:"dhcp,omitempty"`
}

type Netboot struct {
	//+optional
	AllowPXE *bool `json:"allowPXE,omitempty"`

	//+optional
	AllowWorkflow *bool `json:"allowWorkflow,omitempty"`

	//+optional
	IPXE *IPXE `json:"ipxe,omitempty"`

	//+optional
	OSIE *OSIE `json:"osie,omitempty"`
}

type IPXE struct {
	URL      string `json:"url,omitempty"`
	Contents string `json:"contents,omitempty"`
}

type OSIE struct {
	BaseURL string `json:"baseURL,omitempty"`
	Kernel  string `json:"kernel,omitempty"`
	Initrd  string `json:"initrd,omitempty"`
}

type DHCP struct {
	MAC         string   `json:"mac,omitempty"`
	Hostname    string   `json:"hostname,omitempty"`
	LeaseTime   int64    `json:"lease_time,omitempty"`
	NameServers []string `json:"name_servers,omitempty"`
	TimeServers []string `json:"time_servers,omitempty"`
	Arch        string   `json:"arch,omitempty"`
	UEFI        bool     `json:"uefi,omitempty"`
	IfaceName   string   `json:"iface_name,omitempty"`
	IP          *IP      `json:"ip,omitempty"`
}

type IP struct {
	Address string `json:"address,omitempty"`
	Netmask string `json:"netmask,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Family  int64  `json:"family,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=hardware,scope=Cluster,categories=tinkerbell,singular=hardware
// +kubebuilder:storageversion

// Hardware is the Schema for the Hardware API.
type Hardware struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HardwareSpec   `json:"spec,omitempty"`
	Status HardwareStatus `json:"status,omitempty"`
}

// TinkID returns the Tinkerbell ID associated with this Hardware.
func (h *Hardware) TinkID() string {
	return h.Spec.ID
}

// +kubebuilder:object:root=true

// HardwareList contains a list of Hardware.
type HardwareList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Hardware `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Hardware{}, &HardwareList{})
}
