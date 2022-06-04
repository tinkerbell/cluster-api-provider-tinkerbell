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
	"fmt"

	"github.com/google/go-cmp/cmp"
	tinkv1 "github.com/tinkerbell/tink/pkg/apis/core/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const (
	inUse        = "in_use"
	active       = "active"
	provisioning = "provisioning"
	provisioned  = "provisioned"
)

func (mrc *machineReconcileContext) workflowState() (tinkv1.WorkflowState, error) {
	var state tinkv1.WorkflowState

	namespacedName := types.NamespacedName{
		Name:      mrc.tinkerbellMachine.Name,
		Namespace: mrc.tinkerbellMachine.Namespace,
	}
	t := &tinkv1.Workflow{}

	err := mrc.client.Get(mrc.ctx, namespacedName, t)
	if err != nil {
		return state, fmt.Errorf("error getting workflow: %w", err)
	}
	// workflow not found. is this the correct way to check if the workflow exists?
	if apierrors.IsNotFound(err) {
		return state, fmt.Errorf("workflow does not exists: %w", err)
	}

	return t.GetCurrentActionState(), nil
}

type errNilPointer struct {
	name string
}

func (e *errNilPointer) Error() string {
	return fmt.Sprintf("error: %q cannot be nil", e.name)
}

func (e *errNilPointer) isNIL() bool {
	return true
}

// updateState updates the instance state of a machine in the hardware spec.
func updateState(wfState tinkv1.WorkflowState, hw *tinkv1.Hardware) error {
	if hw == nil {
		return &errNilPointer{"hw *tinkv1.Hardware"}
	}

	if hw.Spec.Metadata == nil {
		return &errNilPointer{"metadata *tinkv1.Hardware.Spec.Metadata"}
	}

	if hw.Spec.Metadata.Instance == nil {
		return &errNilPointer{"instance *tinkv1.Hardware.Spec.Metadata.Instance"}
	}

	switch wfState {
	case tinkv1.WorkflowStateRunning:
		hw.Spec.Metadata.Instance.State = provisioning
	case tinkv1.WorkflowStateSuccess:
		hw.Spec.Metadata.Instance.State = provisioned
	default:
		hw.Spec.Metadata.Instance.State = active
	}

	return nil
}

// updateHardwareState sets the state of a machine in the hardware spec at
// Hardware.Spec.Metadata.State and Hardware.Spec.Metadata.Instance.State.
//
// The state is determined by where the machine is in its workflow.
// If a workflow for this machine exists and it is in tinkv1.WorkflowStateRunning,
// then we set Hardware.Spec.Metadata.State = "in_use" and Hardware.Spec.Metadata.Instance.State = "provisioning".
// If a workflow for this machine exists and it is in tinkv1.WorkflowStateSuccess,
// then we set Hardware.Spec.Metadata.State = "in_use" and Hardware.Spec.Metadata.Instance.State = "provisioned".
//
// This is needed so that an already provisioned machine that might be netbooting, due to a reboot for example,
// will not be given any netboot options by Boots.
// In Boots, if the Hardware.Spec.Metadata.State == "in_use"
// and Hardware.Spec.Metadata.Instance.State != "active" then Boots will not provide netboot options to the machine.
// see Boots logic here:
// https://github.com/tinkerbell/boots/blob/505785d758de3879a416ba6e3d49844d64d51a02/job/dhcp.go#L116
func (mrc *machineReconcileContext) updateHardwareState() error {
	wfState, err := mrc.workflowState()
	if err != nil {
		return fmt.Errorf("error getting workflow state: %w", err)
	}

	if wfState == "" {
		return fmt.Errorf("no workflow found for machine %q", mrc.tinkerbellMachine.Name) // nolint:goerr113
	}

	hw, err := mrc.hardwareForMachine()
	if err != nil {
		return fmt.Errorf("error getting hardware: %w", err)
	}

	original := hw.DeepCopy() // nolint:ifshort

	if err := updateState(wfState, hw); err != nil {
		return err
	}

	hw.Spec.Metadata.State = inUse

	if cmp.Equal(hw, original) {
		mrc.log.Info("hardware states have not changed", "hw.Spec.Metadata.State", hw.Spec.Metadata.State, "hw.Spec.Metadata.Instance.State", hw.Spec.Metadata.Instance.State) // nolint: lll

		return nil
	}

	if err := mrc.client.Update(mrc.ctx, hw); err != nil {
		return fmt.Errorf("error patching Hardware object: %w", err)
	}

	return nil
}
