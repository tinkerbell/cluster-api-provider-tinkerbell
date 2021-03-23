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

package client_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	testutils "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/test/utils"
)

func TestWorkflowLifecycle(t *testing.T) { //nolint:paralleltest
	g := NewWithT(t)
	ctx := context.Background()

	templateClient := client.NewTemplateClient(realTemplateClient(t))
	hardwareClient := client.NewHardwareClient(realHardwareClient(t))
	workflowClient := client.NewWorkflowClient(realWorkflowClient(t), hardwareClient)

	// Create a template for the workflow to use
	testTemplate := testutils.GenerateTemplate(rand.String(12), testutils.HelloWorldTemplate)
	g.Expect(templateClient.Create(ctx, testTemplate)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		g.Expect(templateClient.Delete(ctx, testTemplate.Id))
	}()

	// Create hardware for the workflow to use
	testHardware, err := testutils.GenerateHardware(3)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hardwareClient.Create(ctx, testHardware)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		g.Expect(hardwareClient.Delete(ctx, testHardware.Id))
	}()

	// Create the workflow
	workflowID, err := workflowClient.Create(ctx, testTemplate.Id, testHardware.Id)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workflowID).NotTo(BeEmpty())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		g.Expect(workflowClient.Delete(ctx, workflowID))
	}()

	// Get the workflow and verify the values are what we expect
	res, err := workflowClient.Get(ctx, workflowID)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res).NotTo(BeNil())
	g.Expect(res.GetId()).To(BeEquivalentTo(workflowID))
	g.Expect(res.GetTemplate()).To(BeEquivalentTo(testTemplate.Id))

	expectedHardwareJSON, err := client.HardwareToJSON(testHardware)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res.GetHardware()).To(MatchJSON(expectedHardwareJSON))
}
