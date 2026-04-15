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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

// TinkerbellCluster implements webhook interfaces for the TinkerbellCluster API type.
type TinkerbellCluster struct{}

var (
	_ admission.Validator[*infrastructurev1.TinkerbellCluster] = &TinkerbellCluster{}
	_ admission.Defaulter[*infrastructurev1.TinkerbellCluster] = &TinkerbellCluster{}
)

// SetupWebhookWithManager sets up and registers the webhook with the manager.
func (w *TinkerbellCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1.TinkerbellCluster{}).
		WithDefaulter(w).
		WithValidator(w).
		Complete() //nolint:wrapcheck
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tinkerbellcluster,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,versions=v1beta1,name=validation.tinkerbellcluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1
// +kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-tinkerbellcluster,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,versions=v1beta1,name=default.tinkerbellcluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// ValidateCreate implements admission.Validator.
func (w *TinkerbellCluster) ValidateCreate(_ context.Context, _ *infrastructurev1.TinkerbellCluster) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate implements admission.Validator.
func (w *TinkerbellCluster) ValidateUpdate(_ context.Context, _, _ *infrastructurev1.TinkerbellCluster) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete implements admission.Validator.
func (w *TinkerbellCluster) ValidateDelete(_ context.Context, _ *infrastructurev1.TinkerbellCluster) (admission.Warnings, error) {
	return nil, nil
}

// Default implements admission.Defaulter.
func (w *TinkerbellCluster) Default(_ context.Context, _ *infrastructurev1.TinkerbellCluster) error {
	return nil
}
