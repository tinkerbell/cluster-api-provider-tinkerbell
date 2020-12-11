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

const (
	// HardwareIDAnnotation is used by the controller to store the
	// ID assigned to the hardware by Tinkerbell.
	HardwareIDAnnotation = "hardware.tinkerbell.org/id"

	HardwareFinalizer = "hardware.tinkerbell.org"
)

// HardwareSpec defines the desired state of Hardware.
type HardwareSpec struct {
}

// HardwareStatus defines the observed state of Hardware.
type HardwareStatus struct {
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
