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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TinkerbellClusterSpec defines the desired state of TinkerbellCluster
// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
// Important: Run "make" to regenerate code after modifying this file.
type TinkerbellClusterSpec struct {
	// HardwareDiscoveryStrategy is a switch we have to implement more advacned
	// discovery strategy. The unique one we have today is the default one
	// obviously and it uses the two lists of hardware IDs specified down here.
	HardwareDiscoveryStrategy string `json:"hardwareDiscoveryStrategy,omitempty"`
	// ControlPlaneHardwareIDs contains a list of hardware IDs used as pool for
	// control plane kubernetes instances.
	ControlPlaneHardwareIDs []string `json:"controlPlaneHardwareIDs,omitempty"`
	// MachineHardwareIDs contains a list of hardware IDs used as pool for data
	// plane kubernetes instances.
	MachineHardwareIDs []string `json:"machineHardwareIDs,omitempty"`
}

// TinkerbellClusterStatus defines the observed state of TinkerbellCluster.
type TinkerbellClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file.

	// Ready denotes that the cluster (infrastructure) is ready.
	// +optional
	Ready bool `json:"ready"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true

// TinkerbellCluster is the Schema for the tinkerbellclusters API.
type TinkerbellCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinkerbellClusterSpec   `json:"spec,omitempty"`
	Status TinkerbellClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TinkerbellClusterList contains a list of TinkerbellCluster.
type TinkerbellClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinkerbellCluster `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&TinkerbellCluster{}, &TinkerbellClusterList{})
}
