/*
Copyright 2021 The Kubernetes Authors.

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

package v1alpha3

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	osUbuntu             = "ubuntu"
	defaultUbuntuVersion = "20.04"
	defaultOSDistro      = osUbuntu
)

// SetupWebhookWithManager sets up and registers the webhook with the manager.
func (c *TinkerbellCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1alpha3-tinkerbellcluster,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,versions=v1alpha3,name=validation.tinkerbellcluster.infrastructure.cluster.x-k8s.io,sideEffects=None
// +kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1alpha3-tinkerbellcluster,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,versions=v1alpha3,name=default.tinkerbellcluster.infrastructure.cluster.x-k8s.io,sideEffects=None

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (c *TinkerbellCluster) ValidateCreate() error {
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (c *TinkerbellCluster) ValidateUpdate(oldRaw runtime.Object) error {
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (c *TinkerbellCluster) ValidateDelete() error {
	return nil
}

func defaultVersionForOSDistro(distro string) string {
	if strings.ToLower(distro) == osUbuntu {
		return defaultUbuntuVersion
	}

	return ""
}

// Default implements webhookutil.defaulter so a webhook will be registered for the type.
func (c *TinkerbellCluster) Default() {
	if c.Spec.ImageLookupFormat == "" {
		c.Spec.ImageLookupFormat = "{{.BaseURL}}{{.OSDistro}}-{{.OSVersion}}-kube-{{.KubernetesVersion}}.gz"
	}

	if c.Spec.ImageLookupBaseURL == "" {
		tinkIP := os.Getenv("TINKERBELL_IP")
		c.Spec.ImageLookupBaseURL = fmt.Sprintf("http://%s:8080/", tinkIP)
	}

	if c.Spec.ImageLookupOSDistro == "" {
		c.Spec.ImageLookupOSDistro = defaultOSDistro
	}

	if c.Spec.ImageLookupOSVersion == "" {
		c.Spec.ImageLookupOSVersion = defaultVersionForOSDistro(c.Spec.ImageLookupOSDistro)
	}
}
