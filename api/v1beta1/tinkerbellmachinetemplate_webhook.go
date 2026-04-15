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

package v1beta1

import (
	"context"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ admission.Validator[*TinkerbellMachineTemplate] = &TinkerbellMachineTemplate{}

// SetupWebhookWithManager sets up and registers the webhook with the manager.
func (m *TinkerbellMachineTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, m).WithValidator(m).Complete() //nolint:wrapcheck
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tinkerbellmachinetemplate,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellmachinetemplates,versions=v1beta1,name=validation.tinkerbellmachinetemplate.infrastructure.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// ValidateCreate implements admission.Validator so a webhook will be registered for the type.
// HardwareName and ProviderID are structurally prevented from appearing in templates
// because TinkerbellMachineTemplateResource.Spec uses TinkerbellMachineConfig (which
// does not include those fields) instead of TinkerbellMachineSpec.
func (m *TinkerbellMachineTemplate) ValidateCreate(_ context.Context, _ *TinkerbellMachineTemplate) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type.
func (m *TinkerbellMachineTemplate) ValidateUpdate(_ context.Context, oldTMT *TinkerbellMachineTemplate, newTMT *TinkerbellMachineTemplate) (admission.Warnings, error) {
	if !reflect.DeepEqual(newTMT.Spec, oldTMT.Spec) {
		return nil, apierrors.NewBadRequest("TinkerbellMachineTemplate.Spec is immutable")
	}

	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type.
func (m *TinkerbellMachineTemplate) ValidateDelete(_ context.Context, _ *TinkerbellMachineTemplate) (admission.Warnings, error) {
	return nil, nil
}
