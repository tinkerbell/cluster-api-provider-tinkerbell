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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	osUbuntu             = "ubuntu"
	defaultUbuntuVersion = "20.04"
)

var (
	_ webhook.CustomValidator = &TinkerbellCluster{}
	_ webhook.CustomDefaulter = &TinkerbellCluster{}
)

// SetupWebhookWithManager sets up and registers the webhook with the manager.
func (c *TinkerbellCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).WithDefaulter(c).WithValidator(c).For(c).Complete() //nolint:wrapcheck
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tinkerbellcluster,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,versions=v1beta1,name=validation.tinkerbellcluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1
// +kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-tinkerbellcluster,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,versions=v1beta1,name=default.tinkerbellcluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (c *TinkerbellCluster) ValidateCreate(context.Context, runtime.Object) (admission.Warnings, error) {
	return nil, aggregateObjErrors(c.GroupVersionKind().GroupKind(), c.Name, c.validateSpec())
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (c *TinkerbellCluster) ValidateUpdate(_ context.Context, _ runtime.Object, newRaw runtime.Object) (admission.Warnings, error) {
	newCluster, ok := newRaw.(*TinkerbellCluster)
	if !ok {
		return nil, fmt.Errorf("expected TinkerbellCluster, got %T", newRaw)
	}

	return nil, aggregateObjErrors(newCluster.GroupVersionKind().GroupKind(), newCluster.Name, newCluster.validateSpec())
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (c *TinkerbellCluster) ValidateDelete(context.Context, runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func defaultVersionForOSDistro(distro string) string {
	if strings.ToLower(distro) == osUbuntu {
		return defaultUbuntuVersion
	}

	return ""
}

func (c *TinkerbellCluster) validateSpec() field.ErrorList {
	var allErrs field.ErrorList

	if c.Spec.TemplateOverride != "" && c.Spec.TemplateOverrideRef != nil {
		allErrs = append(allErrs,
			field.Invalid(field.NewPath("spec", "templateOverrideRef"), c.Spec.TemplateOverrideRef,
				"templateOverrideRef and templateOverride are mutually exclusive"))
	}

	return allErrs
}

// Default implements webhookutil.defaulter so a webhook will be registered for the type.
func (c *TinkerbellCluster) Default(context.Context, runtime.Object) error {
	if c.Spec.ImageLookupFormat == "" {
		c.Spec.ImageLookupFormat = "{{.BaseRegistry}}/{{.OSDistro}}-{{.OSVersion}}:{{.KubernetesVersion}}.gz"
	}

	if c.Spec.ImageLookupOSVersion == "" {
		c.Spec.ImageLookupOSVersion = defaultVersionForOSDistro(c.Spec.ImageLookupOSDistro)
	}

	return nil
}
