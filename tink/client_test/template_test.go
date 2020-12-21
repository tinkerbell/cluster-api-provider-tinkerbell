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
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	"k8s.io/apimachinery/pkg/util/rand"
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

func TestTemplateLifecycle(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	templateClient := client.NewTemplateClient(realTemplateClient(t))
	name := rand.String(12)

	// Ensure that we now get a NotFound error trying to get the template
	_, err := templateClient.Get(ctx, "", name)
	g.Expect(err).To(MatchError(client.ErrNotFound))

	// Create a template using the hello world template
	testTemplate := generateTemplate(name, helloWorldTemplate)
	g.Expect(templateClient.Create(ctx, testTemplate)).To(Succeed())

	// Ensure that the template now has an ID set
	g.Expect(testTemplate.Id).NotTo(BeEmpty())
	expectedID := testTemplate.Id

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we can delete the template by ID
		g.Expect(templateClient.Delete(ctx, expectedID))

		// Ensure that we now get a NotFound error trying to get the template
		_, err := templateClient.Get(ctx, expectedID, "")
		g.Expect(err).To(MatchError(client.ErrNotFound))
	}()

	// Verify that trying to create a template with a duplicate name fails
	testDuplicateName := generateTemplate(name, sampleTemplate)
	g.Expect(templateClient.Create(ctx, testDuplicateName)).NotTo(Succeed())

	// Ensure we can get the template we just created by ID
	// and it has the values we expect
	resByID, err := templateClient.Get(ctx, expectedID, "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resByID).NotTo(BeNil())
	g.Expect(resByID.Id).To(BeEquivalentTo(expectedID))
	g.Expect(resByID.Name).To(BeEquivalentTo(name))
	g.Expect(resByID.Data).To(BeEquivalentTo(helloWorldTemplate))

	// Ensure we can get the previously created template by Name
	// and it has the values we expect
	resByName, err := templateClient.Get(ctx, "", testTemplate.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resByName).NotTo(BeNil())
	g.Expect(resByName.Id).To(BeEquivalentTo(expectedID))
	g.Expect(resByName.Name).To(BeEquivalentTo(name))
	g.Expect(resByName.Data).To(BeEquivalentTo(helloWorldTemplate))

	// Update the template's data
	testTemplate.Data = sampleTemplate
	g.Expect(templateClient.Update(ctx, testTemplate)).To(Succeed())

	// Esnure that the template was updated in Tinkerbell
	res, err := templateClient.Get(ctx, expectedID, "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res).NotTo(BeNil())
	g.Expect(res.Id).To(BeEquivalentTo(expectedID))
	g.Expect(res.Name).To(BeEquivalentTo(name))
	g.Expect(res.Data).To(BeEquivalentTo(sampleTemplate))
}
