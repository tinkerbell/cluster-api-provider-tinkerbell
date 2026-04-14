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

package machine //nolint:testpackage

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/scheme"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

func readyMachine() *clusterv1.Machine {
	version := "foo"
	dataSecretName := "bar"

	return &clusterv1.Machine{
		Spec: clusterv1.MachineSpec{
			Bootstrap: clusterv1.Bootstrap{
				DataSecretName: &dataSecretName,
			},
			Version: version,
		},
	}
}

func Test_Machine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutateF       func(m *clusterv1.Machine) *clusterv1.Machine
		expectError   bool
		expectedError error
		ready         bool
	}{
		"is_not_ready_when_it_is_nil": {
			mutateF: func(_ *clusterv1.Machine) *clusterv1.Machine {
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
				m.Spec.Version = ""

				return m
			},
			expectError:   true,
			expectedError: ErrMachineVersionEmpty,
		},
		"is_not_valid_when_version_is_empty": {
			mutateF: func(m *clusterv1.Machine) *clusterv1.Machine {
				v := ""
				m.Spec.Version = v

				return m
			},
			expectError:   true,
			expectedError: ErrMachineVersionEmpty,
		},
		"is_ready_when_all_requirements_are_met": {
			ready: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			m := readyMachine()

			if c.mutateF != nil {
				m = c.mutateF(m)
			}

			reason, err := isMachineReady(m)
			if c.expectError {
				g.Expect(err).To(MatchError(c.expectedError))

				return
			}

			g.Expect(err).NotTo(HaveOccurred())

			if c.ready {
				g.Expect(reason).To(BeEmpty(), "Expected ready machine")

				return
			}

			// TODO: should we match reason here?
			g.Expect(reason).NotTo(BeEmpty(), "Expect machine to not be ready")
		})
	}
}

func Test_tinkerbellNamespace(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		targetNamespace   string
		expectedNamespace string
	}{
		"returns_empty_when_nothing_persisted": {
			expectedNamespace: "",
		},
		"returns_persisted_namespace": {
			targetNamespace:   "tinkerbell",
			expectedNamespace: "tinkerbell",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			scope := &machineReconcileScope{
				tinkerbellMachine: &infrastructurev1.TinkerbellMachine{
					Status: infrastructurev1.TinkerbellMachineStatus{
						TargetNamespace: tc.targetNamespace,
					},
				},
			}

			g.Expect(scope.tinkerbellNamespace()).To(Equal(tc.expectedNamespace))
		})
	}
}

func TestSetResourceOwnership(t *testing.T) {
	t.Parallel()

	trueVal := true

	s := runtime.NewScheme()
	if err := infrastructurev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	// Register Tinkerbell types so controllerutil.SetControllerReference
	// can resolve the TinkerbellMachine GVK.
	sb := &scheme.Builder{GroupVersion: tinkv1.GroupVersion}
	sb.Register(&tinkv1.Template{}, &tinkv1.TemplateList{})
	if err := sb.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		external      bool
		objNamespace  string
		wantLabels    map[string]string
		wantOwnerRefs []metav1.OwnerReference
	}{
		"local same namespace sets labels and owner reference": {
			external:     false,
			objNamespace: "mgmt-ns",
			wantLabels: map[string]string{
				LabelMachineName:      "my-machine",
				LabelMachineNamespace: "mgmt-ns",
			},
			wantOwnerRefs: []metav1.OwnerReference{{
				APIVersion:         infrastructurev1.GroupVersion.String(),
				Kind:               "TinkerbellMachine",
				Name:               "my-machine",
				UID:                "abc-123",
				Controller:         &trueVal,
				BlockOwnerDeletion: &trueVal,
			}},
		},
		"local cross namespace sets labels only": {
			external:     false,
			objNamespace: "tinkerbell",
			wantLabels: map[string]string{
				LabelMachineName:      "my-machine",
				LabelMachineNamespace: "mgmt-ns",
			},
		},
		"external mode sets labels only": {
			external:     true,
			objNamespace: "tink-system",
			wantLabels: map[string]string{
				LabelMachineName:      "my-machine",
				LabelMachineNamespace: "mgmt-ns",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scope := &machineReconcileScope{
				tinkerbellMachine: &infrastructurev1.TinkerbellMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-machine",
						Namespace: "mgmt-ns",
						UID:       types.UID("abc-123"),
					},
				},
				externalTinkerbell: tc.external,
				scheme:             s,
			}

			obj := &tinkv1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: tc.objNamespace,
				},
			}
			if err := scope.setResourceOwnership(obj); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.wantLabels, obj.GetLabels()); diff != "" {
				t.Errorf("labels mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantOwnerRefs, obj.GetOwnerReferences()); diff != "" {
				t.Errorf("ownerReferences mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLabelToMachineMapper(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		labels map[string]string
		want   []reconcile.Request
	}{
		"valid labels returns reconcile request": {
			labels: map[string]string{
				LabelMachineName:      "my-machine",
				LabelMachineNamespace: "capi-system",
			},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "my-machine", Namespace: "capi-system"}},
			},
		},
		"missing name label returns nil": {
			labels: map[string]string{
				LabelMachineNamespace: "capi-system",
			},
		},
		"missing namespace label returns nil": {
			labels: map[string]string{
				LabelMachineName: "my-machine",
			},
		},
		"no labels returns nil": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			obj := &tinkv1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workflow",
					Namespace: "tinkerbell",
					Labels:    tc.labels,
				},
			}

			got := labelToMachineMapper(context.Background(), obj)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("reconcile requests mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFinalizerMigration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		initialFinalizers []string
	}{
		"legacy finalizer only": {
			initialFinalizers: []string{infrastructurev1.MachineLegacyFinalizer},
		},
		"new finalizer only": {
			initialFinalizers: []string{infrastructurev1.MachineFinalizer},
		},
		"both finalizers": {
			initialFinalizers: []string{infrastructurev1.MachineLegacyFinalizer, infrastructurev1.MachineFinalizer},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Simulate the removal logic used in releaseHardware and removeFinalizer.
			hw := &tinkv1.Hardware{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-hw",
					Namespace:  "tinkerbell",
					Finalizers: tc.initialFinalizers,
				},
			}

			controllerutil.RemoveFinalizer(hw, infrastructurev1.MachineFinalizer)
			controllerutil.RemoveFinalizer(hw, infrastructurev1.MachineLegacyFinalizer)

			if len(hw.Finalizers) != 0 {
				t.Errorf("expected no finalizers, got %v", hw.Finalizers)
			}
		})
	}
}
