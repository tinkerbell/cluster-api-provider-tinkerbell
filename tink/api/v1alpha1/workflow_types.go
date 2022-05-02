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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// WorkflowIDAnnotation is used by the controller to store the
	// ID assigned to the workflow by Tinkerbell.
	WorkflowIDAnnotation = "workflow.tinkerbell.org/id"

	// WorkflowFinalizer is used by the controller to ensure
	// proper deletion of the workflow resource.
	WorkflowFinalizer = "workflow.tinkerbell.org"
)

// WorkflowSpec defines the desired state of Workflow.
type WorkflowSpec struct {
	// Name of the Template associated with this workflow.
	TemplateRef string `json:"templateRef,omitempty"`

	// Name of the Hardware associated with this workflow.
	HardwareRef string `json:"hardwareRef,omitempty"`
}

// WorkflowStatus defines the observed state of Workflow.
type WorkflowStatus struct {
	// State is the state of the workflow in Tinkerbell.
	State string `json:"state,omitempty"`

	// Data is the populated Workflow Data in Tinkerbell.
	Data string `json:"data,omitempty"`

	// Metadata is the metadata stored in Tinkerbell.
	Metadata string `json:"metadata,omitempty"`

	// Actions are the actions for this Workflow.
	Actions []Action `json:"actions,omitempty"`

	// Events are events for this Workflow.
	Events []Event `json:"events,omitempty"`
}

// Action represents a workflow action.
type Action struct {
	TaskName    string   `json:"task_name,omitempty"`
	Name        string   `json:"name,omitempty"`
	Image       string   `json:"image,omitempty"`
	Timeout     int64    `json:"timeout,omitempty"`
	Command     []string `json:"command,omitempty"`
	OnTimeout   []string `json:"on_timeout,omitempty"`
	OnFailure   []string `json:"on_failure,omitempty"`
	WorkerID    string   `json:"worker_id,omitempty"`
	Volumes     []string `json:"volumes,omitempty"`
	Environment []string `json:"environment,omitempty"`
}

// Event represents a workflow event.
type Event struct {
	TaskName     string      `json:"task_name,omitempty"`
	ActionName   string      `json:"action_name,omitempty"`
	ActionStatus string      `json:"action_status,omitempty"`
	Seconds      int64       `json:"seconds,omitempty"`
	Message      string      `json:"message,omitempty"`
	CreatedAt    metav1.Time `json:"created_at,omitempty"`
	WorkerID     string      `json:"worker_id,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=workflows,scope=Cluster,categories=tinkerbell
// +kubebuilder:storageversion

// Workflow is the Schema for the Workflows API.
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkflowSpec   `json:"spec,omitempty"`
	Status WorkflowStatus `json:"status,omitempty"`
}

// TinkID returns the Tinkerbell ID associated with this Workflow.
func (w *Workflow) TinkID() string {
	annotations := w.GetAnnotations()
	if len(annotations) == 0 {
		return ""
	}

	return annotations[WorkflowIDAnnotation]
}

// SetTinkID sets the Tinkerbell ID associated with this Workflow.
func (w *Workflow) SetTinkID(id string) {
	if w.GetAnnotations() == nil {
		w.SetAnnotations(make(map[string]string))
	}

	w.Annotations[WorkflowIDAnnotation] = id
}

// +kubebuilder:object:root=true

// WorkflowList contains a list of Workflows.
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&Workflow{}, &WorkflowList{})
}
