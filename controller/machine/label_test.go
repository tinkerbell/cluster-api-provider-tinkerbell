package machine

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rufiov1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
)

func TestExternalLabelMapper(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		labels   map[string]string
		expected []reconcile.Request
		obj      client.Object
	}{
		"both labels present": {
			labels: map[string]string{
				LabelMachineName:      "my-machine",
				LabelMachineNamespace: "my-namespace",
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "my-machine", Namespace: "my-namespace"}},
			},
		},
		"missing name label": {
			labels: map[string]string{
				LabelMachineNamespace: "my-namespace",
			},
			expected: nil,
		},
		"missing namespace label": {
			labels: map[string]string{
				LabelMachineName: "my-machine",
			},
			expected: nil,
		},
		"both labels missing": {
			labels:   map[string]string{"unrelated": "label"},
			expected: nil,
		},
		"empty string values": {
			labels: map[string]string{
				LabelMachineName:      "",
				LabelMachineNamespace: "",
			},
			expected: nil,
		},
		"nil labels": {
			labels:   nil,
			expected: nil,
		},
		"non-Workflow object": {
			obj: &rufiov1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
					Labels: map[string]string{
						LabelMachineName:      "job-machine",
						LabelMachineNamespace: "job-ns",
					},
				},
			},
			expected: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "job-machine", Namespace: "job-ns"}},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.obj == nil {
				tc.obj = &tinkv1.Workflow{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-workflow",
						Labels: tc.labels,
					},
				}
			}

			got := externalLabelMapper(context.Background(), tc.obj)
			if len(tc.expected) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected nil/empty, got %v", got)
				}
				return
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d requests, got %d", len(tc.expected), len(got))
			}
			if got[0].NamespacedName != tc.expected[0].NamespacedName {
				t.Fatalf("expected %v, got %v", tc.expected[0].NamespacedName, got[0].NamespacedName)
			}
		})
	}
}
