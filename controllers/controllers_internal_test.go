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

package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	tinkv1 "github.com/tinkerbell/tink/pkg/apis/core/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

type isNIL interface {
	isNIL() bool
}

// isNilPointer returns true if err is a nil pointer error.
func isNilPointer(err error) bool {
	n, ok := err.(isNIL) // nolint:errorlint

	return ok && n.isNIL()
}

func validHardware(name, uuid, ip string) *tinkv1.Hardware {
	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "myClusterNamespace",
			UID:       types.UID(uuid),
		},
		Spec: tinkv1.HardwareSpec{
			Disks: []tinkv1.Disk{
				{
					Device: "/dev/sda",
				},
			},
			Interfaces: []tinkv1.Interface{
				{
					DHCP: &tinkv1.DHCP{
						IP: &tinkv1.IP{
							Address: ip,
						},
					},
				},
			},
			Metadata: &tinkv1.HardwareMetadata{
				Instance: &tinkv1.MetadataInstance{
					ID: ip,
				},
			},
		},
	}

	return hw
}

func TestErrNilPointer(t *testing.T) { // nolint:paralleltest
	got := &errNilPointer{"foo"}
	if diff := cmp.Diff(got.Error(), "error: \"foo\" cannot be nil"); diff != "" {
		t.Fatal(diff)
	}

	if !isNilPointer(got) {
		t.Fatal("expected errNilPointer to be a nil pointer error")
	}
}

func TestUpdateInstanceState(t *testing.T) { // nolint:paralleltest
	tests := map[string]struct {
		wfState tinkv1.WorkflowState
		hw      *tinkv1.Hardware
		want    *tinkv1.Hardware
	}{
		"nil hw":       {},
		"nil metadata": {hw: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: nil}}, want: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: nil}}},
		"nil instance": {hw: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: nil}}}, want: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: nil}}}},
		"hw updated provisioning": {
			wfState: tinkv1.WorkflowStateRunning,
			hw:      &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{}}}},
			want:    &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: provisioning}}}},
		},
		"hw updated provisioned": {
			wfState: tinkv1.WorkflowStateSuccess,
			hw:      &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{}}}},
			want:    &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: provisioned}}}},
		},
		"hw not updated": {
			wfState: tinkv1.WorkflowStatePending,
			hw:      &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: active}}}},
			want:    &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: active}}}},
		},
	}

	for name, tt := range tests { // nolint:paralleltest
		t.Run(name, func(t *testing.T) {
			err := updateInstanceState(tt.wfState, tt.hw)
			if err != nil && !isNilPointer(err) {
				t.Fatalf("updateState(%v, %v) = %v, want nil pointer err", tt.wfState, tt.hw, err)
			}
			if diff := cmp.Diff(tt.hw, tt.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func kubernetesClientWithObjects(t *testing.T, objects []runtime.Object) client.Client {
	t.Helper()
	// g := NewWithT(t)

	scheme := runtime.NewScheme()

	if err := tinkv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	if err := clusterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
}

func createWorkflow(name, uuid string, state tinkv1.WorkflowState) *tinkv1.Workflow {
	wf := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "myClusterNamespace",
			UID:       types.UID(uuid),
		},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: uuid,
		},
		Status: tinkv1.WorkflowStatus{
			State: state,
			Tasks: []tinkv1.Task{
				{
					Name: name,
					Actions: []tinkv1.Action{
						{
							Name:   name,
							Status: state,
						},
					},
				},
			},
		},
	}

	return wf
}

func validTemplate(name, uuid string) *tinkv1.Template {
	return &tinkv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "myClusterNamespace",
			UID:       types.UID(uuid),
		},
		Spec: tinkv1.TemplateSpec{},
	}
}

// nolint:paralleltest
func TestWorkflowState(t *testing.T) {
	tests := map[string]struct {
		wfState tinkv1.WorkflowState
	}{
		"pending": {wfState: tinkv1.WorkflowStatePending},
		"running": {wfState: tinkv1.WorkflowStateRunning},
		"empty":   {wfState: tinkv1.WorkflowState("")},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			one := uuid.New().String()
			objects := []runtime.Object{
				validHardware("machine1", one, "192.168.2.5"),
				validTemplate("template1", one),
				createWorkflow("machine1", one, tt.wfState),
			}

			client := kubernetesClientWithObjects(t, objects)

			mrc := machineReconcileContext{
				baseMachineReconcileContext: &baseMachineReconcileContext{
					log:    logr.Discard(),
					ctx:    context.Background(),
					client: client,
					tinkerbellMachine: &v1beta1.TinkerbellMachine{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "machine1",
							Namespace: "myClusterNamespace",
						},
					},
				},
			}

			got, err := mrc.workflowState()
			if err != nil {
				t.Fatalf("workflowState() = %v, want nil", err)
			}
			if diff := cmp.Diff(tt.wfState, got); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

// nolint:paralleltest
func TestWorkflowStateErrors(t *testing.T) {
	one := uuid.New().String()
	objects := []runtime.Object{
		validHardware("machine1", one, "192.168.2.5"),
		validTemplate("template1", one),
	}

	client := kubernetesClientWithObjects(t, objects)

	mrc := machineReconcileContext{
		baseMachineReconcileContext: &baseMachineReconcileContext{
			log:    logr.Discard(),
			ctx:    context.Background(),
			client: client,
			tinkerbellMachine: &v1beta1.TinkerbellMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "machine1",
					Namespace: "myClusterNamespace",
				},
			},
		},
	}

	if _, err := mrc.workflowState(); err == nil {
		t.Fatal("workflowState() = nil, want error")
	}
}

// nolint:paralleltest
func TestSetHardwareState(t *testing.T) {
	tests := map[string]struct {
		metadataState string
		instanceState string
		state         tinkv1.WorkflowState
	}{
		"pending": {metadataState: "in_use", instanceState: active, state: tinkv1.WorkflowStatePending},
		"running": {metadataState: "in_use", instanceState: provisioning, state: tinkv1.WorkflowStateRunning},
		"failed":  {metadataState: "in_use", instanceState: active, state: tinkv1.WorkflowStateFailed},
		"timeout": {metadataState: "in_use", instanceState: active, state: tinkv1.WorkflowStateTimeout},
		"success": {metadataState: "in_use", instanceState: provisioned, state: tinkv1.WorkflowStateSuccess},
		"empty":   {metadataState: "in_use", instanceState: provisioned, state: tinkv1.WorkflowState("")},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			one := uuid.New().String()
			hw := validHardware("machine1", one, "192.168.2.5")
			objects := []runtime.Object{
				hw,
				validTemplate("template1", one),
				createWorkflow("machine1", one, tt.state),
			}

			client := kubernetesClientWithObjects(t, objects)

			mrc := machineReconcileContext{
				baseMachineReconcileContext: &baseMachineReconcileContext{
					log:    logr.Discard(),
					ctx:    context.Background(),
					client: client,
					tinkerbellMachine: &v1beta1.TinkerbellMachine{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "machine1",
							Namespace: "myClusterNamespace",
						},
					},
				},
			}
			err := mrc.setHardwareState(hw)
			if err != nil {
				t.Fatalf("setHardwareState() = %v, want nil", err)
			}
			if diff := cmp.Diff(tt.metadataState, hw.Spec.Metadata.State); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff(tt.instanceState, hw.Spec.Metadata.Instance.State); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

/*
func TestSetHardwareStateErrors(t *testing.T) {
	tests := map[string]struct {
		metadataState string
		instanceState string
		state         tinkv1.WorkflowState
	}{
		"pending": {metadataState: "in_use", instanceState: active, state: tinkv1.WorkflowStatePending},
		"running": {metadataState: "in_use", instanceState: provisioning, state: tinkv1.WorkflowStateRunning},
		"failed":  {metadataState: "in_use", instanceState: active, state: tinkv1.WorkflowStateFailed},
		"timeout": {metadataState: "in_use", instanceState: active, state: tinkv1.WorkflowStateTimeout},
		"success": {metadataState: "in_use", instanceState: provisioned, state: tinkv1.WorkflowStateSuccess},
		"empty":   {metadataState: "in_use", instanceState: provisioned, state: tinkv1.WorkflowState("")},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			one := uuid.New().String()
			hw := validHardware("machine1", one, "192.168.2.5")
			objects := []runtime.Object{
				hw,
				validTemplate("template1", one),
				createWorkflow("machine1", one, tt.state),
			}

			client := kubernetesClientWithObjects(t, objects)

			mrc := machineReconcileContext{
				baseMachineReconcileContext: &baseMachineReconcileContext{
					log:    logr.Discard(),
					ctx:    context.Background(),
					client: client,
					tinkerbellMachine: &v1beta1.TinkerbellMachine{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "machine1",
							Namespace: "myClusterNamespace",
						},
					},
				},
			}
			err := mrc.setHardwareState(hw)
			if err != nil {
				t.Fatalf("setHardwareState() = %v, want nil", err)
			}
			if diff := cmp.Diff(tt.metadataState, hw.Spec.Metadata.State); diff != "" {
				t.Fatal(diff)
			}
			if diff := cmp.Diff(tt.instanceState, hw.Spec.Metadata.Instance.State); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
*/
