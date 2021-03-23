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

// Package controllers contains controllers for Tinkerbell.
package template

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/tinkerbell/tink/protos/template"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake" //nolint:staticcheck
	"sigs.k8s.io/controller-runtime/pkg/log"

	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	tinkfake "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client/fake"
)

const helloWorldTemplate = `version: "0.1"
name: hello_world_workflow
global_timeout: 600
tasks:
  - name: "hello world"
    worker: "{{.device_1}}"
    actions:
      - name: "hello_world"
        image: hello-world
        timeout: 60`

//nolint:funlen
func TestTemplateReconciler_reconcileDelete(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	now := metav1.Now()

	g.Expect(tinkv1alpha1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name     string
		in       *tinkv1alpha1.Template
		tinkObjs []*template.WorkflowTemplate
		want     ctrl.Result
		wantErr  bool
	}{
		{
			name: "successful delete by id",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test",
					DeletionTimestamp: &now,
					Finalizers:        []string{tinkv1alpha1.TemplateFinalizer},
					Annotations: map[string]string{
						tinkv1alpha1.TemplateIDAnnotation: "testId",
					},
				},
			},
			tinkObjs: []*template.WorkflowTemplate{
				{
					Id:   "testId",
					Name: "test",
				},
			},
			want:    ctrl.Result{},
			wantErr: false,
		},
		{
			name: "template doesn't exist in tinkerbell",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test",
					DeletionTimestamp: &now,
					Finalizers:        []string{tinkv1alpha1.TemplateFinalizer},
					Annotations: map[string]string{
						tinkv1alpha1.TemplateIDAnnotation: "testId",
					},
				},
			},
			tinkObjs: []*template.WorkflowTemplate{},
			want:     ctrl.Result{},
			wantErr:  false,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			fakeTemplateClient := tinkfake.NewFakeTemplateClient(tt.tinkObjs...)

			r := &Reconciler{
				Client:         fake.NewFakeClientWithScheme(scheme, tt.in.DeepCopy()),
				TemplateClient: fakeTemplateClient,
				Log:            log.Log,
				Scheme:         scheme,
			}

			got, err := r.reconcileDelete(context.Background(), tt.in)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())

				return
			}
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(got).To(BeEquivalentTo(tt.want))

			// Verify that the deletion happened in the fakeTemplateServiceClient
			g.Expect(fakeTemplateClient.Objs).NotTo(HaveKey(tt.in.Name))

			// Check for absence of a finalizer since the fake client doesn't
			// do automatic deletion
			key := client.ObjectKey{Name: tt.in.Name}
			after := &tinkv1alpha1.Template{}
			g.Expect(r.Client.Get(context.Background(), key, after)).To(Succeed())
			g.Expect(after.Finalizers).NotTo(ContainElement(tinkv1alpha1.TemplateFinalizer))
		})
	}
}

//nolint:funlen
func TestTemplateReconciler_reconcileNormal(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	scheme := runtime.NewScheme()

	g.Expect(tinkv1alpha1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name     string
		in       *tinkv1alpha1.Template
		tinkObjs []*template.WorkflowTemplate
		want     ctrl.Result
		wantErr  bool
	}{
		{
			name: "successful create",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			tinkObjs: nil,
			want:     ctrl.Result{},
			wantErr:  false,
		},
		{
			name: "successful update",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						tinkv1alpha1.TemplateIDAnnotation: "testId",
					},
				},
				Spec: tinkv1alpha1.TemplateSpec{
					Data: pointer.StringPtr(helloWorldTemplate),
				},
			},
			tinkObjs: []*template.WorkflowTemplate{
				{
					Id:   "testId",
					Name: "test",
				},
			},
			want:    ctrl.Result{},
			wantErr: false,
		},
		{
			name: "successful adopt",
			in: &tinkv1alpha1.Template{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			tinkObjs: []*template.WorkflowTemplate{
				{
					Id:   "testId",
					Name: "test",
					Data: helloWorldTemplate,
				},
			},
			want:    ctrl.Result{},
			wantErr: false,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			fakeTemplateClient := tinkfake.NewFakeTemplateClient(tt.tinkObjs...)

			r := &Reconciler{
				Client:         fake.NewFakeClientWithScheme(scheme, tt.in.DeepCopy()),
				TemplateClient: fakeTemplateClient,
				Log:            log.Log,
				Scheme:         scheme,
			}

			got, err := r.reconcileNormal(context.Background(), tt.in)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())

				return
			}
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(got).To(BeEquivalentTo(tt.want))

			// get the k8s resource from the fake client
			k8sTemplate := &tinkv1alpha1.Template{}
			key := client.ObjectKey{Name: tt.in.Name}
			g.Expect(r.Client.Get(context.Background(), key, k8sTemplate)).To(Succeed())

			id := k8sTemplate.TinkID()

			// Expect the id to be non-empty
			g.Expect(id).NotTo(BeEmpty())

			// Verify that the resource exists in the fakeTemplateServiceClient
			g.Expect(fakeTemplateClient.Objs).To(HaveKey(id))

			// get the tink resource from the fake client
			tinkTemplate := fakeTemplateClient.Objs[id]

			// Verify the IDs match
			g.Expect(tinkTemplate.Id, id)

			// Verify the Names match
			g.Expect(tinkTemplate.Name).To(BeEquivalentTo(k8sTemplate.Name))

			// Verify the Data matches
			g.Expect(k8sTemplate.Spec.Data).NotTo(BeNil())
			g.Expect(tinkTemplate.Data).To(BeEquivalentTo(*k8sTemplate.Spec.Data))
		})
	}
}
