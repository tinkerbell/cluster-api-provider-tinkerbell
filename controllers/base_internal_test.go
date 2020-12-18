/*
Copyright 2020 The Kubernetes Authors.

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
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

func readyMachine() *clusterv1.Machine {
	version := "foo"
	dataSecretName := "bar"

	return &clusterv1.Machine{
		Spec: clusterv1.MachineSpec{
			Bootstrap: clusterv1.Bootstrap{
				DataSecretName: &dataSecretName,
			},
			Version: &version,
		},
	}
}

//nolint:funlen
func Test_Machine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutateF     func(m *clusterv1.Machine) *clusterv1.Machine
		expectError bool
		ready       bool
	}{
		"is_not_ready_when_it_is_nil": {
			mutateF: func(m *clusterv1.Machine) *clusterv1.Machine {
				return nil
			},
		},
		"is_not_ready_when_bootstrap_reference_is_not_set": {
			mutateF: func(m *clusterv1.Machine) *clusterv1.Machine {
				m.Spec.Bootstrap.DataSecretName = nil

				return m
			},
		},
		"is_not_valid_when_version_is_not_set": {
			mutateF: func(m *clusterv1.Machine) *clusterv1.Machine {
				m.Spec.Version = nil

				return m
			},
			expectError: true,
		},
		"is_not_valid_when_version_is_empty": {
			mutateF: func(m *clusterv1.Machine) *clusterv1.Machine {
				v := ""
				m.Spec.Version = &v

				return m
			},
			expectError: true,
		},
		"is_ready_when_all_requirements_are_met": {
			ready: true,
		},
	}

	for name, c := range cases {
		c := c

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := readyMachine()

			if c.mutateF != nil {
				m = c.mutateF(m)
			}

			reason, err := isMachineReady(m)

			if c.expectError && err == nil {
				t.Fatalf("Expected error")
			}

			if !c.expectError && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if c.ready && reason != "" {
				t.Fatalf("Expected ready machine, got: %v", reason)
			}

			if !c.ready && err == nil && reason == "" {
				t.Fatalf("Expect machine to not be ready")
			}
		})
	}
}
