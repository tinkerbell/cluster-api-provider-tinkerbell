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

// Package conversion registers CRD version conversion for the infrastructure
// API types using controller-runtime's registry-based conversion mechanism.
package conversion

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	crconversion "sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	infrastructurev1beta1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta2"
)

// NewTinkerbellClusterConverter returns a converter constructor for TinkerbellCluster.
func NewTinkerbellClusterConverter() func(*runtime.Scheme) (crconversion.Converter, error) {
	return crconversion.NewHubSpokeConverter(
		&infrastructurev1.TinkerbellCluster{},
		crconversion.NewSpokeConverter(
			&infrastructurev1beta1.TinkerbellCluster{},
			func(_ context.Context, hub *infrastructurev1.TinkerbellCluster, spoke *infrastructurev1beta1.TinkerbellCluster) error {
				return infrastructurev1beta1.ConvertClusterFromHub(spoke, hub)
			},
			func(_ context.Context, spoke *infrastructurev1beta1.TinkerbellCluster, hub *infrastructurev1.TinkerbellCluster) error {
				return infrastructurev1beta1.ConvertClusterToHub(spoke, hub)
			},
		),
	)
}

// NewTinkerbellMachineConverter returns a converter constructor for TinkerbellMachine.
func NewTinkerbellMachineConverter() func(*runtime.Scheme) (crconversion.Converter, error) {
	return crconversion.NewHubSpokeConverter(
		&infrastructurev1.TinkerbellMachine{},
		crconversion.NewSpokeConverter(
			&infrastructurev1beta1.TinkerbellMachine{},
			func(_ context.Context, hub *infrastructurev1.TinkerbellMachine, spoke *infrastructurev1beta1.TinkerbellMachine) error {
				return infrastructurev1beta1.ConvertMachineFromHub(spoke, hub)
			},
			func(_ context.Context, spoke *infrastructurev1beta1.TinkerbellMachine, hub *infrastructurev1.TinkerbellMachine) error {
				return infrastructurev1beta1.ConvertMachineToHub(spoke, hub)
			},
		),
	)
}

// NewTinkerbellMachineTemplateConverter returns a converter constructor for TinkerbellMachineTemplate.
func NewTinkerbellMachineTemplateConverter() func(*runtime.Scheme) (crconversion.Converter, error) {
	return crconversion.NewHubSpokeConverter(
		&infrastructurev1.TinkerbellMachineTemplate{},
		crconversion.NewSpokeConverter(
			&infrastructurev1beta1.TinkerbellMachineTemplate{},
			func(_ context.Context, hub *infrastructurev1.TinkerbellMachineTemplate, spoke *infrastructurev1beta1.TinkerbellMachineTemplate) error {
				return infrastructurev1beta1.ConvertMachineTemplateFromHub(spoke, hub)
			},
			func(_ context.Context, spoke *infrastructurev1beta1.TinkerbellMachineTemplate, hub *infrastructurev1.TinkerbellMachineTemplate) error {
				return infrastructurev1beta1.ConvertMachineTemplateToHub(spoke, hub)
			},
		),
	)
}
