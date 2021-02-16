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

package sources_test

import (
	"context"
	"io"
	"os"
	"testing"

	. "github.com/onsi/gomega"
	tinkv1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/sources"
	testutils "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/test/utils"
	rawclient "github.com/tinkerbell/tink/client"
	"github.com/tinkerbell/tink/protos/events"
	"github.com/tinkerbell/tink/protos/hardware"
	"github.com/tinkerbell/tink/protos/template"
	"github.com/tinkerbell/tink/protos/workflow"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/klogr"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	sampleTemplate = `
version: '0.1'
name: sample_workflow
global_timeout: 600
tasks:
  - name: "hello world first"
    worker: "{{.device_1}}"
    actions:
      - name: "hello_world_first"
        image: hello-world
        timeout: 60
  - name: "hello world second"
    worker: "{{.device_1}}"
    actions:
      - name: "hello_world_second"
        image: hello-world
        timeout: 60`
)

func TestHardwareEvents(t *testing.T) { //nolint: funlen,paralleltest
	t.Skip("Skipping test until eventing works with CAPT")
	g := NewWithT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheme := runtime.NewScheme()
	g.Expect(tinkv1.AddToScheme(scheme)).To(Succeed())

	fakeClient := fake.NewFakeClientWithScheme(scheme)
	conn := realConn(t)
	hardwareClient := client.NewHardwareClient(hardware.NewHardwareServiceClient(conn))
	rawclient.EventsClient = events.NewEventsServiceClient(conn)
	eventCh := make(chan event.GenericEvent)

	ctrl.SetLogger(klogr.New())

	eventWatcher := sources.TinkEventWatcher{
		Logger:       log.Log,
		EventCh:      eventCh,
		ResourceType: events.ResourceType_RESOURCE_TYPE_HARDWARE,
	}

	g.Expect(eventWatcher.InjectClient(fakeClient)).To(Succeed())

	go func() {
		g.Expect(eventWatcher.Start(ctx.Done())).Should(SatisfyAny(Succeed(), MatchError(io.EOF)))
	}()

	// Create a Hardware resource in Tinkerbell
	testHardware, err := testutils.GenerateHardware(2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hardwareClient.Create(ctx, testHardware)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we clean up the hardware resource we just created
		g.Expect(hardwareClient.Delete(ctx, testHardware.Id))
	}()

	// Since we don't have a matching hardware resource in k8s
	// we shouldn't see an event
	g.Consistently(eventCh).ShouldNot(Receive())

	// Create the hardware resource in k8s
	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: tinkv1.HardwareSpec{
			ID: testHardware.Id,
		},
	}
	g.Expect(fakeClient.Create(ctx, hw)).To(Succeed())

	// Update the hardware
	additionalInterface, err := testutils.GenerateHardwareInterface("")
	g.Expect(err).NotTo(HaveOccurred())

	testHardware.Network.Interfaces = append(testHardware.Network.Interfaces, additionalInterface)
	g.Expect(hardwareClient.Update(ctx, testHardware)).To(Succeed())

	expectedEvent := event.GenericEvent{
		Meta:   hw,
		Object: hw,
	}
	g.Eventually(eventCh).Should(Receive(BeEquivalentTo(expectedEvent)))
}

func TestTemplateEvents(t *testing.T) { //nolint: funlen,paralleltest
	t.Skip("Skipping test until eventing works with CAPT")
	g := NewWithT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheme := runtime.NewScheme()
	g.Expect(tinkv1.AddToScheme(scheme)).To(Succeed())

	fakeClient := fake.NewFakeClientWithScheme(scheme)
	conn := realConn(t)
	templateClient := client.NewTemplateClient(template.NewTemplateServiceClient(conn))
	rawclient.EventsClient = events.NewEventsServiceClient(conn)
	eventCh := make(chan event.GenericEvent)

	ctrl.SetLogger(klogr.New())

	eventWatcher := sources.TinkEventWatcher{
		Logger:       log.Log,
		EventCh:      eventCh,
		ResourceType: events.ResourceType_RESOURCE_TYPE_TEMPLATE,
	}

	g.Expect(eventWatcher.InjectClient(fakeClient)).To(Succeed())

	go func() {
		g.Expect(eventWatcher.Start(ctx.Done())).Should(SatisfyAny(Succeed(), MatchError(io.EOF)))
	}()

	// Create the Template resource in Tinkerbell
	testTemplate := testutils.GenerateTemplate("testTemplate", testutils.HelloWorldTemplate)
	g.Expect(templateClient.Create(ctx, testTemplate)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we clean up the hardware resource we just created
		g.Expect(templateClient.Delete(ctx, testTemplate.Id))
	}()

	// Since we don't have a matching template resource in k8s
	// we shouldn't see an event
	g.Consistently(eventCh).ShouldNot(Receive())

	// Create the matching k8s resource
	k8sTemplate := &tinkv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name: testTemplate.Name,
			Annotations: map[string]string{
				tinkv1.TemplateIDAnnotation: testTemplate.Id,
			},
		},
		Spec: tinkv1.TemplateSpec{
			Data: pointer.StringPtr(testTemplate.Data),
		},
	}
	g.Expect(fakeClient.Create(ctx, k8sTemplate)).To(Succeed())

	// Update the template
	testTemplate.Data = sampleTemplate
	g.Expect(templateClient.Update(ctx, testTemplate)).To(Succeed())

	expectedEvent := event.GenericEvent{
		Meta:   k8sTemplate,
		Object: k8sTemplate,
	}
	g.Eventually(eventCh).Should(Receive(BeEquivalentTo(expectedEvent)))
}

func TestWorkflowEvents(t *testing.T) { //nolint: funlen,paralleltest
	t.Skip("Skipping test until eventing works with CAPT")

	g := NewWithT(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheme := runtime.NewScheme()
	g.Expect(tinkv1.AddToScheme(scheme)).To(Succeed())

	fakeClient := fake.NewFakeClientWithScheme(scheme)
	conn := realConn(t)
	hardwareClient := client.NewHardwareClient(hardware.NewHardwareServiceClient(conn))
	templateClient := client.NewTemplateClient(template.NewTemplateServiceClient(conn))
	rawWorkflowClient := workflow.NewWorkflowServiceClient(conn)
	workflowClient := client.NewWorkflowClient(rawWorkflowClient, hardwareClient)
	rawclient.EventsClient = events.NewEventsServiceClient(conn)
	eventCh := make(chan event.GenericEvent)

	ctrl.SetLogger(klogr.New())

	eventWatcher := sources.TinkEventWatcher{
		Logger:       log.Log,
		EventCh:      eventCh,
		ResourceType: events.ResourceType_RESOURCE_TYPE_WORKFLOW,
	}

	g.Expect(eventWatcher.InjectClient(fakeClient)).To(Succeed())

	go func() {
		g.Expect(eventWatcher.Start(ctx.Done())).Should(SatisfyAny(Succeed(), MatchError(io.EOF)))
	}()

	// Create a Hardware resource in Tinkerbell
	testHardware, err := testutils.GenerateHardware(2)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hardwareClient.Create(ctx, testHardware)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we clean up the hardware resource we just created
		g.Expect(hardwareClient.Delete(ctx, testHardware.Id))
	}()

	// Create the Template resource in Tinkerbell
	testTemplate := testutils.GenerateTemplate("testTemplate", testutils.HelloWorldTemplate)
	g.Expect(templateClient.Create(ctx, testTemplate)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we clean up the template resource we just created
		g.Expect(templateClient.Delete(ctx, testTemplate.Id))
	}()

	// Create the Workflow resource in Tinkerbell
	workflowID, err := workflowClient.Create(ctx, testTemplate.Id, testHardware.Id)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workflowID).NotTo(BeEmpty())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we clean up the workflow resource we just created
		g.Expect(workflowClient.Delete(ctx, workflowID))
	}()

	// Since we don't have a matching template resource in k8s
	// we shouldn't see an event
	g.Consistently(eventCh).ShouldNot(Receive())

	// Create the matching k8s resources
	k8sTemplate := &tinkv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name: testTemplate.Name,
			Annotations: map[string]string{
				tinkv1.TemplateIDAnnotation: testTemplate.Id,
			},
		},
		Spec: tinkv1.TemplateSpec{
			Data: pointer.StringPtr(testTemplate.Data),
		},
	}

	g.Expect(fakeClient.Create(ctx, k8sTemplate)).To(Succeed())

	h := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testHardware",
		},
		Spec: tinkv1.HardwareSpec{
			ID: testHardware.Id,
		},
	}

	g.Expect(fakeClient.Create(ctx, h)).To(Succeed())

	w := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testWorkflow",
			Annotations: map[string]string{
				tinkv1.WorkflowIDAnnotation: workflowID,
			},
		},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: k8sTemplate.Name,
			HardwareRef: h.Name,
		},
	}
	g.Expect(fakeClient.Create(ctx, w)).To(Succeed())
}

func realConn(t *testing.T) *grpc.ClientConn { //nolint: unused
	t.Helper()
	g := NewWithT(t)

	certURL, ok := os.LookupEnv("TINKERBELL_CERT_URL")
	if !ok || certURL == "" {
		t.Skip("Skipping live client tests because TINKERBELL_CERT_URL is not set.")
	}

	grpcAuthority, ok := os.LookupEnv("TINKERBELL_GRPC_AUTHORITY")
	if !ok || grpcAuthority == "" {
		t.Skip("Skipping live client tests because TINKERBELL_GRPC_AUTHORITY is not set.")
	}

	conn, err := rawclient.GetConnection()
	g.Expect(err).NotTo(HaveOccurred())

	return conn
}
