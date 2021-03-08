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

package common_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/common"
)

func Test_EnsureFinalizer(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	scheme := runtime.NewScheme()

	g.Expect(tinkv1alpha1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name      string
		in        common.Object
		finalizer string
		wantErr   bool
	}{
		{
			name: "Adds finalizer when not present",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: tinkv1alpha1.TemplateSpec{},
			},
			finalizer: "my-test-finalizer",
			wantErr:   false,
		},
		{
			name: "Finalizer already exists",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Finalizers: []string{"my-test-finalizer"},
				},
				Spec: tinkv1alpha1.TemplateSpec{},
			},
			finalizer: "my-test-finalizer",
			wantErr:   false,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := context.Background()
			fakeClient := fake.NewFakeClientWithScheme(scheme, tt.in.DeepCopyObject())

			err := common.EnsureFinalizer(ctx, fakeClient, log.Log, tt.in, tt.finalizer)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())

				return
			}
			g.Expect(err).NotTo(HaveOccurred())

			actual := &tinkv1alpha1.Template{}
			key := client.ObjectKey{Name: tt.in.GetName()}
			g.Expect(fakeClient.Get(ctx, key, actual)).To(Succeed())
			g.Expect(actual.Finalizers).To(ContainElement(tt.finalizer))
		})
	}
}
