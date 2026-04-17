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

package webhooks

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta2"
)

// TinkerbellMachine implements webhook interfaces for the TinkerbellMachine API type.
type TinkerbellMachine struct{}

var _ admission.Validator[*infrastructurev1.TinkerbellMachine] = &TinkerbellMachine{}

// SetupWebhookWithManager sets up and registers the webhook with the manager.
func (w *TinkerbellMachine) SetupWebhookWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewWebhookManagedBy(mgr, &infrastructurev1.TinkerbellMachine{}).
		WithValidator(w).
		Complete(); err != nil {
		return fmt.Errorf("setting up TinkerbellMachine webhook: %w", err)
	}

	return nil
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta2-tinkerbellmachine,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellmachines,versions=v1beta2,name=validation.tinkerbellmachine.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// ValidateCreate implements admission.Validator.
func (w *TinkerbellMachine) ValidateCreate(_ context.Context, obj *infrastructurev1.TinkerbellMachine) (admission.Warnings, error) {
	allErrs := validateMachineSpec(&obj.Spec)

	return nil, aggregateObjErrors(obj.GroupVersionKind().GroupKind(), obj.Name, allErrs)
}

// ValidateUpdate implements admission.Validator.
func (w *TinkerbellMachine) ValidateUpdate(_ context.Context, old *infrastructurev1.TinkerbellMachine, newTM *infrastructurev1.TinkerbellMachine) (admission.Warnings, error) {
	allErrs := validateMachineSpec(&newTM.Spec)

	if old.Spec.HardwareName != "" && newTM.Spec.HardwareName != old.Spec.HardwareName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "hardwareName"), "is immutable once set"))
	}

	if old.Spec.ProviderID != "" && newTM.Spec.ProviderID != old.Spec.ProviderID {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "providerID"), "is immutable once set"))
	}

	return nil, aggregateObjErrors(newTM.GroupVersionKind().GroupKind(), newTM.Name, allErrs)
}

// ValidateDelete implements admission.Validator.
func (w *TinkerbellMachine) ValidateDelete(_ context.Context, _ *infrastructurev1.TinkerbellMachine) (admission.Warnings, error) {
	return nil, nil
}

func validateMachineSpec(spec *infrastructurev1.TinkerbellMachineSpec) field.ErrorList {
	var allErrs field.ErrorList

	fieldBasePath := field.NewPath("spec")

	if spec.HardwareAffinity != nil {
		for i, term := range spec.HardwareAffinity.Preferred {
			if term.Weight < 1 || term.Weight > 100 {
				allErrs = append(allErrs,
					field.Invalid(fieldBasePath.Child("HardwareAffinity", "Preferred").Index(i),
						term.Weight, "must be in the range [1,100]"))
			}
		}
	}

	return allErrs
}
