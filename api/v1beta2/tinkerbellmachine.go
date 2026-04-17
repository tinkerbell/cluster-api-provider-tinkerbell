/*
Copyright 2022 The Tinkerbell Authors.

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

package v1beta2

import (
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// MachineFinalizer allows ReconcileTinkerbellMachine to clean up Tinkerbell resources before
	// removing it from the apiserver.
	MachineFinalizer = "infrastructure.cluster.x-k8s.io/tinkerbellmachine"

	// MachineLegacyFinalizer is the old finalizer name without a path separator.
	// Kept for backward-compatible removal during upgrades.
	MachineLegacyFinalizer = "tinkerbellmachine.infrastructure.cluster.x-k8s.io"
)

// TinkerbellMachineConfig contains user-configurable fields that define how a machine
// should be provisioned. These fields are safe to set in a TinkerbellMachineTemplate
// and will be copied into each TinkerbellMachine created from the template.
//
// +kubebuilder:validation:XValidation:rule="!has(self.templateInline) || !has(self.templateRef)",message="templateInline and templateRef are mutually exclusive"
type TinkerbellMachineConfig struct {
	// TemplateInline is an inline Tinkerbell Template definition for this machine.
	// Takes precedence over hardware annotations and cluster-level templates.
	// Mutually exclusive with TemplateRef.
	//
	// +optional
	TemplateInline string `json:"templateInline,omitempty"`

	// TemplateRef is a reference to an existing Tinkerbell Template object whose spec.data will
	// be used as the template for this machine. Takes precedence over hardware annotations and
	// cluster-level templates. Mutually exclusive with TemplateInline.
	// When Namespace is omitted, defaults to the Hardware's namespace.
	//
	// +optional
	TemplateRef *ObjectRef `json:"templateRef,omitempty"`

	// HardwareAffinity allows filtering for hardware.
	// +optional
	HardwareAffinity *HardwareAffinity `json:"hardwareAffinity,omitempty"`

	// BootOptions are options that control the booting of Hardware.
	// +optional
	BootOptions BootOptions `json:"bootOptions,omitempty"`
}

// TinkerbellMachineSpec defines the desired state of TinkerbellMachine.
// It embeds TinkerbellMachineConfig (user-intent fields from the template) and adds
// controller-managed fields that are set at runtime during hardware selection.
// The embedded fields flatten into the JSON spec, so the CRD schema is unchanged.
type TinkerbellMachineSpec struct {
	TinkerbellMachineConfig `json:",inline"`

	// HardwareName is the name of the Hardware resource selected for this machine.
	// Set by the controller during hardware selection — not user-configurable.
	// Immutable once set (enforced by webhook).
	HardwareName string `json:"hardwareName,omitempty"`

	// ProviderID is the unique identifier for this machine instance.
	// Format: tinkerbell://<namespace>/<hardware-name>
	// Set by the controller during hardware selection — not user-configurable.
	// Immutable once set (enforced by webhook).
	// Part of the CAPI InfrastructureMachine contract (must be in spec).
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	ProviderID string `json:"providerID,omitempty"`
}

// BootOptions are options that control the booting of Hardware.
type BootOptions struct {
	// ISOURL is the URL of the ISO that will be one-time booted.
	// A HardwareRef that contains a spec.BmcRef must be provided.
	//
	// The format of the ISOURL must be http://$IP:$Port/iso/hook.iso
	// The name of the ISO file must have the .iso extension, but the name can be anything.
	// The $IP and $Port should generally point to the IP and Port of the Smee server
	// as this is where the ISO patching endpoint lives.
	// The controller will append the MAC address of the hardware in the ISO URL
	// right before the iso file name in the URL.
	// MAC address is then used to retrieve hardware specific information such as
	// IPAM info, custom kernel cmd line args and populate the worker ID for the tink worker/agent.
	// For ex. the above format would be replaced to http://$IP:$Port/iso/<macAddress>/hook.iso
	//
	// BootMode must be set to "isoboot".
	// +optional
	// +kubebuilder:validation:Format=uri
	ISOURL string `json:"isoURL,omitempty"`

	// BootMode is the type of booting that will be done. One of "netboot", "isoboot", or "customboot".
	// +optional
	// +kubebuilder:validation:Enum=netboot;isoboot;iso;customboot
	BootMode tinkv1.BootMode `json:"bootMode,omitempty"`

	// CustombootConfig is the configuration for the "customboot" boot mode.
	// This allows users to define custom BMC Actions.
	CustombootConfig tinkv1.CustombootConfig `json:"custombootConfig,omitempty,omitzero"`
}

// HardwareAffinity defines the required and preferred hardware affinities.
type HardwareAffinity struct {
	// Required are the required hardware affinity terms.  The terms are OR'd together, hardware must match one term to
	// be considered.
	// +optional
	Required []HardwareAffinityTerm `json:"required,omitempty"`
	// Preferred are the preferred hardware affinity terms. Hardware matching these terms are preferred according to the
	// weights provided, but are not required.
	// +optional
	Preferred []WeightedHardwareAffinityTerm `json:"preferred,omitempty"`
}

// HardwareAffinityTerm is used to select for a particular existing hardware resource.
type HardwareAffinityTerm struct {
	// LabelSelector is used to select for particular hardware by label.
	LabelSelector metav1.LabelSelector `json:"labelSelector"`
}

// WeightedHardwareAffinityTerm is a HardwareAffinityTerm with an associated weight.  The weights of all the matched
// WeightedHardwareAffinityTerm fields are added per-hardware to find the most preferred hardware.
type WeightedHardwareAffinityTerm struct {
	// Weight associated with matching the corresponding hardwareAffinityTerm, in the range 1-100.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`
	// HardwareAffinityTerm is the term associated with the corresponding weight.
	HardwareAffinityTerm HardwareAffinityTerm `json:"hardwareAffinityTerm"`
}

// TinkerbellMachineStatus defines the observed state of TinkerbellMachine.
type TinkerbellMachineStatus struct {
	// Ready is true when the provider resource is ready.
	// +optional
	Ready bool `json:"ready"`

	// Initialization provides observations of the TinkerbellMachine initialization process.
	// NOTE: Fields in this struct are part of the Cluster API contract and are used to orchestrate initial Machine provisioning.
	// +optional
	Initialization *TinkerbellMachineInitializationStatus `json:"initialization,omitempty"`

	// Addresses contains the Tinkerbell device associated addresses.
	Addresses []corev1.NodeAddress `json:"addresses,omitempty"`

	// State is the status of the Tinkerbell device instance for this machine.
	// +optional
	State *TinkerbellResourceStatus `json:"state,omitempty"`

	// TargetNamespace is the resolved namespace where Tinkerbell resources
	// (Template, Workflow, Job) for this machine are created.
	// It is computed once during hardware selection and persisted so that subsequent
	// reconcile loops (including deletion when the Hardware object may be gone) always
	// target the correct namespace without re-deriving it.
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Conditions defines current service state of the TinkerbellMachine.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	ErrorReason *string `json:"errorReason,omitempty"`

	// ErrorMessage will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a more verbose string suitable
	// for logging and human consumption.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

// TinkerbellMachineInitializationStatus provides observations of the TinkerbellMachine initialization process.
// +kubebuilder:validation:MinProperties=1
type TinkerbellMachineInitializationStatus struct {
	// provisioned is true when the infrastructure provider reports that the Machine's infrastructure is fully provisioned.
	// NOTE: this field is part of the Cluster API contract, and it is used to orchestrate initial Machine provisioning.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// GetConditions returns the conditions for the TinkerbellMachine.
func (m *TinkerbellMachine) GetConditions() []metav1.Condition {
	return m.Status.Conditions
}

// SetConditions sets the conditions on the TinkerbellMachine.
func (m *TinkerbellMachine) SetConditions(conditions []metav1.Condition) {
	m.Status.Conditions = conditions
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tinkerbellmachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this TinkerbellMachine belongs"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="Tinkerbell instance state"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Machine ready status"
// +kubebuilder:printcolumn:name="InstanceID",type="string",JSONPath=".spec.providerID",description="Tinkerbell instance ID"
// +kubebuilder:printcolumn:name="Machine",type="string",JSONPath=".metadata.ownerReferences[?(@.kind==\"Machine\")].name",description="Machine object which owns with this TinkerbellMachine"

// TinkerbellMachine is the Schema for the tinkerbellmachines API.
type TinkerbellMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinkerbellMachineSpec   `json:"spec,omitempty"`
	Status TinkerbellMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TinkerbellMachineList contains a list of TinkerbellMachine.
type TinkerbellMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinkerbellMachine `json:"items"`
}
