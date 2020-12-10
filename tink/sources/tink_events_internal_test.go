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

// Package sources contains custom controller-runtime sources for Tinkerbell.
package sources

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	tinkv1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	tinkevents "github.com/tinkerbell/tink/protos/events"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestTinkEventWatcher_generateEventForTinkID(t *testing.T) { //nolint:funlen
	g := NewWithT(t)
	scheme := runtime.NewScheme()

	g.Expect(tinkv1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name          string
		id            string
		resourceType  tinkevents.ResourceType
		objs          []runtime.Object
		eventExpected bool
		want          event.GenericEvent
		wantErr       bool
	}{
		{
			name:          "hardware not found",
			id:            "doesNotExist",
			resourceType:  tinkevents.ResourceType_RESOURCE_TYPE_HARDWARE,
			eventExpected: false,
			wantErr:       false,
		},
		{
			name: "hardware found",
			id:   "foo",
			objs: []runtime.Object{
				&tinkv1.Hardware{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: tinkv1.HardwareSpec{
						ID: "foo",
					},
				},
			},
			resourceType:  tinkevents.ResourceType_RESOURCE_TYPE_HARDWARE,
			eventExpected: true,
			want: event.GenericEvent{
				Meta: &tinkv1.Hardware{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: tinkv1.HardwareSpec{
						ID: "foo",
					},
				},
				Object: &tinkv1.Hardware{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: tinkv1.HardwareSpec{
						ID: "foo",
					},
				},
			},
			wantErr: false,
		},
		{
			name:          "template not found",
			id:            "doesNotExist",
			resourceType:  tinkevents.ResourceType_RESOURCE_TYPE_TEMPLATE,
			eventExpected: false,
			wantErr:       false,
		},
		{
			name: "template found",
			id:   "foo",
			objs: []runtime.Object{
				&tinkv1.Template{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Annotations: map[string]string{
							tinkv1.TemplateIDAnnotation: "foo",
						},
					},
				},
			},
			resourceType:  tinkevents.ResourceType_RESOURCE_TYPE_TEMPLATE,
			eventExpected: true,
			want: event.GenericEvent{
				Meta: &tinkv1.Template{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Annotations: map[string]string{
							tinkv1.TemplateIDAnnotation: "foo",
						},
					},
				},
				Object: &tinkv1.Template{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Annotations: map[string]string{
							tinkv1.TemplateIDAnnotation: "foo",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:          "workflow not found",
			id:            "doesNotExist",
			resourceType:  tinkevents.ResourceType_RESOURCE_TYPE_WORKFLOW,
			eventExpected: false,
			wantErr:       false,
		},
		{
			name: "workflow found",
			id:   "foo",
			objs: []runtime.Object{
				&tinkv1.Workflow{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Annotations: map[string]string{
							tinkv1.WorkflowIDAnnotation: "foo",
						},
					},
				},
			},
			resourceType:  tinkevents.ResourceType_RESOURCE_TYPE_WORKFLOW,
			eventExpected: true,
			want: event.GenericEvent{
				Meta: &tinkv1.Workflow{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Annotations: map[string]string{
							tinkv1.WorkflowIDAnnotation: "foo",
						},
					},
				},
				Object: &tinkv1.Workflow{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
						Annotations: map[string]string{
							tinkv1.WorkflowIDAnnotation: "foo",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:         "unknown resource type",
			id:           "doesNotExist",
			resourceType: tinkevents.ResourceType_RESOURCE_TYPE_UNKNOWN,
			wantErr:      true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			eventCh := make(chan event.GenericEvent)

			w := &TinkEventWatcher{
				client:       fake.NewFakeClientWithScheme(scheme, tt.objs...),
				EventCh:      eventCh,
				Logger:       log.Log,
				ResourceType: tt.resourceType,
			}

			go func() {
				err := w.generateEventForTinkID(context.Background(), tt.id)
				if tt.wantErr {
					g.Expect(err).To(HaveOccurred())

					return
				}
				g.Expect(err).NotTo(HaveOccurred())
			}()

			if tt.eventExpected {
				g.Eventually(eventCh).Should(Receive(BeEquivalentTo(tt.want)))
			} else {
				g.Consistently(eventCh).ShouldNot(Receive())
			}
		})
	}
}
