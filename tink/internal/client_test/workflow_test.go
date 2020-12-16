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
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/internal/client"
	"k8s.io/apimachinery/pkg/util/rand"
)

func TestWorkflowLifecycle(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	templateClient := client.NewTemplateClient(realTemplateClient(t))
	_ = realWorkflowClient(t)

	// Create a template for the workflow to use
	testTemplate := generateTemplate(rand.String(12), helloWorldTemplate)
	g.Expect(templateClient.Create(ctx, testTemplate)).To(Succeed())

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we can delete the template by ID
		g.Expect(templateClient.Delete(ctx, testTemplate.Id))
	}()

	// TODO: Create hardware for the workflow to use

	// TODO: Create the workflow

	// TODO: Ensure we can get the workflow we just created by ID
	// and that it has the values we expect

	// TODO: Delete the workflow
	t.Error("not ready yet")
}
