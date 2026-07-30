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

// TinkerbellResourceStatus describes the status of a Tinkerbell resource.
type TinkerbellResourceStatus int

// TinkerbellResourceStatus constants define the lifecycle states of a Tinkerbell resource.
const (
	TinkerbellResourceStatusPending TinkerbellResourceStatus = iota
	TinkerbellResourceStatusRunning
	TinkerbellResourceStatusFailed
	TinkerbellResourceStatusTimeout
	TinkerbellResourceStatusSuccess
)

// TinkerbellMachineTemplateResource describes the data needed to create a TinkerbellMachine
// from a template. It uses TinkerbellMachineConfig instead of TinkerbellMachineSpec because
// templates must not contain controller-managed runtime fields (HardwareName, ProviderID).
//
// The CAPI MachineDeployment controller copies spec.template.spec from the
// InfrastructureMachineTemplate into the new InfrastructureMachine.spec as raw JSON.
// Because TinkerbellMachineConfig is embedded (json:",inline") in TinkerbellMachineSpec,
// the JSON paths are identical — CAPI's copy produces a valid TinkerbellMachineSpec
// with HardwareName and ProviderID absent (zero-valued), which is correct since
// the controller sets them later during hardware selection.
type TinkerbellMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec TinkerbellMachineConfig `json:"spec"`
}
