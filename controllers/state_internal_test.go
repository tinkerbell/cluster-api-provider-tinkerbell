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

package controllers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	tinkv1 "github.com/tinkerbell/tink/pkg/apis/core/v1alpha1"
)

type isNIL interface {
	isNIL() bool
}

// isNilPointer returns true if err is a nil pointer error.
func isNilPointer(err error) bool {
	n, ok := err.(isNIL) // nolint:errorlint

	return ok && n.isNIL()
}

func TestErrNilPointer(t *testing.T) { // nolint:paralleltest
	got := &errNilPointer{"foo"}
	if diff := cmp.Diff(got.Error(), "error: \"foo\" cannot be nil"); diff != "" {
		t.Fatal(diff)
	}

	if !isNilPointer(got) {
		t.Fatal("expected errNilPointer to be a nil pointer error")
	}
}

func TestUpdateState(t *testing.T) { // nolint:paralleltest
	tests := map[string]struct {
		wfState tinkv1.WorkflowState
		hw      *tinkv1.Hardware
		want    *tinkv1.Hardware
	}{
		"nil hw":       {},
		"nil metadata": {hw: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: nil}}, want: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: nil}}},
		"nil instance": {hw: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: nil}}}, want: &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: nil}}}},
		"hw updated provisioning": {
			wfState: tinkv1.WorkflowStateRunning,
			hw:      &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{}}}},
			want:    &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: provisioning}}}},
		},
		"hw updated provisioned": {
			wfState: tinkv1.WorkflowStateSuccess,
			hw:      &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{}}}},
			want:    &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: provisioned}}}},
		},
		"hw not updated": {
			wfState: tinkv1.WorkflowStatePending,
			hw:      &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: active}}}},
			want:    &tinkv1.Hardware{Spec: tinkv1.HardwareSpec{Metadata: &tinkv1.HardwareMetadata{Instance: &tinkv1.MetadataInstance{State: active}}}},
		},
	}

	for name, tt := range tests { // nolint:paralleltest
		t.Run(name, func(t *testing.T) {
			err := updateState(tt.wfState, tt.hw)
			if err != nil && !isNilPointer(err) {
				t.Fatalf("updateState(%v, %v) = %v, want nil pointer err", tt.wfState, tt.hw, err)
			}
			if diff := cmp.Diff(tt.hw, tt.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
