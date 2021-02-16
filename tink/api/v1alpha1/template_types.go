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

// TemplateState represents the template state.
type TemplateState string

const (
	// TemplateError represents a template that is in an error state.
	TemplateError = TemplateState("Error")

	// TemplateReady represents a template that is in a ready state.
	TemplateReady = TemplateState("Ready")

	// TemplateIDAnnotation is used by the controller to store the
	// ID assigned to the template by Tinkerbell.
	TemplateIDAnnotation = "template.tinkerbell.org/id"

	// TemplateFinalizer is used by the controller to ensure
	// proper deletion of the template resource.
	TemplateFinalizer = "template.tinkerbell.org"
)

// TemplateSpec defines the desired state of Template.
type TemplateSpec struct {
	// +optional
	Data *string `json:"data,omitempty"`
}

// TemplateStatus defines the observed state of Template.
type TemplateStatus struct {
	State TemplateState `json:"state,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=templates,scope=Cluster,categories=tinkerbell
// +kubebuilder:storageversion

// Template is the Schema for the Templates API.
type Template struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemplateSpec   `json:"spec,omitempty"`
	Status TemplateStatus `json:"status,omitempty"`
}

// TinkID returns the Tinkerbell ID associated with this Template.
func (t *Template) TinkID() string {
	annotations := t.GetAnnotations()
	if len(annotations) == 0 {
		return ""
	}

	return annotations[TemplateIDAnnotation]
}

// SetTinkID sets the Tinkerbell ID associated with this Template.
func (t *Template) SetTinkID(id string) {
	if t.GetAnnotations() == nil {
		t.SetAnnotations(make(map[string]string))
	}

	t.Annotations[TemplateIDAnnotation] = id
}

// +kubebuilder:object:root=true

// TemplateList contains a list of Templates.
type TemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Template `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&Template{}, &TemplateList{})
}
