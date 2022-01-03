package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BMCState represents the template state.
type BMCState string

const (
	// BMCPowerOn represents a bmc that is in Power On state.
	BMCPowerOn = BMCState("PowerOn")
)

// BMCSpec defines the desired state of BMC.
type BMCSpec struct {
	// Host is the host IP address of the BMC
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// AuthSecretRef is the SecretReference that contains authentication information of the BMC.
	// The Secret must contain username and password keys.
	AuthSecretRef corev1.SecretReference `json:"authSecretRef"`

	// Vendor is the vendor name of the BMC
	// +kubebuilder:validation:MinLength=1
	Vendor string `json:"vendor"`
}

// BMCStatus defines the observed state of BMC.
type BMCStatus struct {
	State BMCState `json:"state,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=bmc,scope=Cluster,categories=tinkerbell,singular=bmc
// +kubebuilder:storageversion

// BMC is the Schema for the BMC API.
type BMC struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BMCSpec   `json:"spec,omitempty"`
	Status BMCStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BMCList contains a list of BMCs.
type BMCList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BMC `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&BMC{}, &BMCList{})
}
