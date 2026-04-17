/*
Copyright The Tinkerbell Authors.

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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"

	infrav2 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta2"
)

// dataAnnotation is the annotation key that conversion logic uses to retain
// hub-only data during down-conversion so that a subsequent up-conversion is
// lossless. This matches the constant defined by sigs.k8s.io/cluster-api/util/conversion.
const dataAnnotation = "cluster.x-k8s.io/conversion-data"

// marshalData serializes src (minus metadata) into a JSON annotation on dst.
// This preserves hub-only fields when down-converting to a spoke version.
func marshalData(src, dst metav1.Object) error {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(src)
	if err != nil {
		return err
	}
	delete(u, "metadata")

	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	annotations := dst.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[dataAnnotation] = string(data)
	dst.SetAnnotations(annotations)
	return nil
}

// unmarshalData retrieves stashed JSON from the annotation and deserializes it
// into to. Returns (true, nil) if data was found, (false, nil) if absent.
func unmarshalData(from metav1.Object, to interface{}) (bool, error) {
	annotations := from.GetAnnotations()
	data, ok := annotations[dataAnnotation]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal([]byte(data), to); err != nil {
		return false, err
	}
	delete(annotations, dataAnnotation)
	from.SetAnnotations(annotations)
	return true, nil
}

// ConvertClusterToHub converts a v1beta1 TinkerbellCluster to the hub version (v1beta2).
func ConvertClusterToHub(src *TinkerbellCluster, dst *infrav2.TinkerbellCluster) error {
	dst.ObjectMeta = src.ObjectMeta

	// Spec field renames.
	dst.Spec.ControlPlaneEndpoint = src.Spec.ControlPlaneEndpoint
	dst.Spec.TemplateInline = src.Spec.TemplateOverride
	if src.Spec.TemplateOverrideRef != nil {
		dst.Spec.TemplateRef = &infrav2.ObjectRef{
			Name:      src.Spec.TemplateOverrideRef.Name,
			Namespace: src.Spec.TemplateOverrideRef.Namespace,
		}
	}

	// Status — identical structure.
	dst.Status.Ready = src.Status.Ready
	dst.Status.Initialization = convertClusterInitToHub(src.Status.Initialization)
	dst.Status.Conditions = src.Status.Conditions

	// Preserve v1beta1-only fields (imageLookup*) so a round-trip is lossless.
	restored := &infrav2.TinkerbellCluster{}
	if ok, err := unmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		// Restore any hub-only fields that were stashed in a previous ConvertFrom.
		// Currently there are no v1beta2-only fields on TinkerbellCluster,
		// but this preserves the pattern for future additions.
		_ = restored
	}

	return nil
}

// ConvertClusterFromHub converts from the hub version (v1beta2) to a v1beta1 TinkerbellCluster.
func ConvertClusterFromHub(dst *TinkerbellCluster, src *infrav2.TinkerbellCluster) error {
	dst.ObjectMeta = src.ObjectMeta

	// Spec field renames (reverse).
	dst.Spec.ControlPlaneEndpoint = src.Spec.ControlPlaneEndpoint
	dst.Spec.TemplateOverride = src.Spec.TemplateInline
	if src.Spec.TemplateRef != nil {
		dst.Spec.TemplateOverrideRef = &ObjectRef{
			Name:      src.Spec.TemplateRef.Name,
			Namespace: src.Spec.TemplateRef.Namespace,
		}
	}

	// Status.
	dst.Status.Ready = src.Status.Ready
	dst.Status.Initialization = convertClusterInitFromHub(src.Status.Initialization)
	dst.Status.Conditions = src.Status.Conditions

	// Stash the full hub object so that v1beta2-only fields survive a round-trip.
	return marshalData(src, dst)
}

// ConvertMachineToHub converts a v1beta1 TinkerbellMachine to the hub version (v1beta2).
func ConvertMachineToHub(src *TinkerbellMachine, dst *infrav2.TinkerbellMachine) error {
	dst.ObjectMeta = src.ObjectMeta

	// Spec — renamed and restructured fields.
	dst.Spec.TemplateInline = src.Spec.TemplateOverride
	// v1beta1 machine had no TemplateRef — leave dst.Spec.TemplateRef nil unless restored below.
	dst.Spec.HardwareAffinity = convertHardwareAffinityToHub(src.Spec.HardwareAffinity)
	dst.Spec.BootOptions = convertBootOptionsToHub(src.Spec.BootOptions)
	dst.Spec.HardwareName = src.Spec.HardwareName
	dst.Spec.ProviderID = src.Spec.ProviderID

	// Status — field rename: InstanceStatus → State.
	dst.Status.Ready = src.Status.Ready
	dst.Status.Initialization = convertMachineInitToHub(src.Status.Initialization)
	dst.Status.Addresses = src.Status.Addresses
	dst.Status.State = convertResourceStatusToHub(src.Status.InstanceStatus)
	dst.Status.TargetNamespace = src.Status.TargetNamespace
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.ErrorReason = src.Status.ErrorReason
	dst.Status.ErrorMessage = src.Status.ErrorMessage

	// Restore v1beta2-only fields stashed during a previous ConvertFrom.
	restored := &infrav2.TinkerbellMachine{}
	if ok, err := unmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		dst.Spec.TemplateRef = restored.Spec.TemplateRef
	}

	return nil
}

// ConvertMachineFromHub converts from the hub version (v1beta2) to a v1beta1 TinkerbellMachine.
func ConvertMachineFromHub(dst *TinkerbellMachine, src *infrav2.TinkerbellMachine) error {
	dst.ObjectMeta = src.ObjectMeta

	// Spec — reverse renames.
	dst.Spec.TemplateOverride = src.Spec.TemplateInline
	// TemplateRef is v1beta2-only — stashed via MarshalData below.
	dst.Spec.HardwareAffinity = convertHardwareAffinityFromHub(src.Spec.HardwareAffinity)
	dst.Spec.BootOptions = convertBootOptionsFromHub(src.Spec.BootOptions)
	dst.Spec.HardwareName = src.Spec.HardwareName
	dst.Spec.ProviderID = src.Spec.ProviderID

	// Status — reverse rename: State → InstanceStatus.
	dst.Status.Ready = src.Status.Ready
	dst.Status.Initialization = convertMachineInitFromHub(src.Status.Initialization)
	dst.Status.Addresses = src.Status.Addresses
	dst.Status.InstanceStatus = convertResourceStatusFromHub(src.Status.State)
	dst.Status.TargetNamespace = src.Status.TargetNamespace
	dst.Status.Conditions = src.Status.Conditions
	dst.Status.ErrorReason = src.Status.ErrorReason
	dst.Status.ErrorMessage = src.Status.ErrorMessage

	// Stash the full hub object for lossless round-trip.
	return marshalData(src, dst)
}

// ConvertMachineTemplateToHub converts a v1beta1 TinkerbellMachineTemplate to the hub version (v1beta2).
func ConvertMachineTemplateToHub(src *TinkerbellMachineTemplate, dst *infrav2.TinkerbellMachineTemplate) error {
	dst.ObjectMeta = src.ObjectMeta

	// In v1beta1, template spec is TinkerbellMachineSpec (includes HardwareName/ProviderID).
	// In v1beta2, it is TinkerbellMachineConfig (excludes them).
	// Drop HardwareName/ProviderID on up-conversion — they should never be set in templates.
	srcSpec := src.Spec.Template.Spec
	dst.Spec.Template.Spec.TemplateInline = srcSpec.TemplateOverride
	// v1beta1 machine template had no TemplateRef — leave nil unless restored.
	dst.Spec.Template.Spec.HardwareAffinity = convertHardwareAffinityToHub(srcSpec.HardwareAffinity)
	dst.Spec.Template.Spec.BootOptions = convertBootOptionsToHub(srcSpec.BootOptions)

	// Restore v1beta2-only fields.
	restored := &infrav2.TinkerbellMachineTemplate{}
	if ok, err := unmarshalData(src, restored); err != nil {
		return err
	} else if ok {
		dst.Spec.Template.Spec.TemplateRef = restored.Spec.Template.Spec.TemplateRef
	}

	return nil
}

// ConvertMachineTemplateFromHub converts from the hub version (v1beta2) to a v1beta1 TinkerbellMachineTemplate.
func ConvertMachineTemplateFromHub(dst *TinkerbellMachineTemplate, src *infrav2.TinkerbellMachineTemplate) error {
	dst.ObjectMeta = src.ObjectMeta

	// v1beta2 TinkerbellMachineConfig → v1beta1 TinkerbellMachineSpec.
	// HardwareName and ProviderID default to zero values.
	srcSpec := src.Spec.Template.Spec
	dst.Spec.Template.Spec.TemplateOverride = srcSpec.TemplateInline
	// TemplateRef is v1beta2-only — stashed via MarshalData.
	dst.Spec.Template.Spec.HardwareAffinity = convertHardwareAffinityFromHub(srcSpec.HardwareAffinity)
	dst.Spec.Template.Spec.BootOptions = convertBootOptionsFromHub(srcSpec.BootOptions)

	return marshalData(src, dst)
}

// --- helpers ---

func convertClusterInitToHub(in *TinkerbellClusterInitializationStatus) *infrav2.TinkerbellClusterInitializationStatus {
	if in == nil {
		return nil
	}
	return &infrav2.TinkerbellClusterInitializationStatus{Provisioned: in.Provisioned}
}

func convertClusterInitFromHub(in *infrav2.TinkerbellClusterInitializationStatus) *TinkerbellClusterInitializationStatus {
	if in == nil {
		return nil
	}
	return &TinkerbellClusterInitializationStatus{Provisioned: in.Provisioned}
}

func convertMachineInitToHub(in *TinkerbellMachineInitializationStatus) *infrav2.TinkerbellMachineInitializationStatus {
	if in == nil {
		return nil
	}
	return &infrav2.TinkerbellMachineInitializationStatus{Provisioned: in.Provisioned}
}

func convertMachineInitFromHub(in *infrav2.TinkerbellMachineInitializationStatus) *TinkerbellMachineInitializationStatus {
	if in == nil {
		return nil
	}
	return &TinkerbellMachineInitializationStatus{Provisioned: in.Provisioned}
}

func convertHardwareAffinityToHub(in *HardwareAffinity) *infrav2.HardwareAffinity {
	if in == nil {
		return nil
	}
	out := &infrav2.HardwareAffinity{}
	for _, r := range in.Required {
		out.Required = append(out.Required, infrav2.HardwareAffinityTerm{LabelSelector: r.LabelSelector})
	}
	for _, p := range in.Preferred {
		out.Preferred = append(out.Preferred, infrav2.WeightedHardwareAffinityTerm{
			Weight:               p.Weight,
			HardwareAffinityTerm: infrav2.HardwareAffinityTerm{LabelSelector: p.HardwareAffinityTerm.LabelSelector},
		})
	}
	return out
}

func convertHardwareAffinityFromHub(in *infrav2.HardwareAffinity) *HardwareAffinity {
	if in == nil {
		return nil
	}
	out := &HardwareAffinity{}
	for _, r := range in.Required {
		out.Required = append(out.Required, HardwareAffinityTerm{LabelSelector: r.LabelSelector})
	}
	for _, p := range in.Preferred {
		out.Preferred = append(out.Preferred, WeightedHardwareAffinityTerm{
			Weight:               p.Weight,
			HardwareAffinityTerm: HardwareAffinityTerm{LabelSelector: p.HardwareAffinityTerm.LabelSelector},
		})
	}
	return out
}

func convertBootOptionsToHub(in BootOptions) infrav2.BootOptions {
	return infrav2.BootOptions{
		ISOURL:           in.ISOURL,
		BootMode:         in.BootMode,
		CustombootConfig: in.CustombootConfig,
	}
}

func convertBootOptionsFromHub(in infrav2.BootOptions) BootOptions {
	return BootOptions{
		ISOURL:           in.ISOURL,
		BootMode:         in.BootMode,
		CustombootConfig: in.CustombootConfig,
	}
}

func convertResourceStatusToHub(in *TinkerbellResourceStatus) *infrav2.TinkerbellResourceStatus {
	if in == nil {
		return nil
	}
	out := infrav2.TinkerbellResourceStatus(*in)
	return &out
}

func convertResourceStatusFromHub(in *infrav2.TinkerbellResourceStatus) *TinkerbellResourceStatus {
	if in == nil {
		return nil
	}
	out := TinkerbellResourceStatus(*in)
	return &out
}
