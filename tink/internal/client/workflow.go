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

package client

import (
	"context"
	"fmt"

	"github.com/tinkerbell/tink/protos/workflow"
)

// Workflow client for Tinkerbell.
type Workflow struct {
	client workflow.WorkflowServiceClient
}

// NewWorkflowClient returns a Workflow client.
func NewWorkflowClient(client workflow.WorkflowServiceClient) Workflow {
	return Workflow{client: client}
}

// Get returns a Tinkerbell Workflow.
func (t *Workflow) Get(ctx context.Context, id string) (*workflow.Workflow, error) {
	tinkWorkflow, err := t.client.GetWorkflow(ctx, &workflow.GetRequest{Id: id})
	if err != nil {
		if err.Error() == sqlErrorString {
			return nil, fmt.Errorf("workflow %w", ErrNotFound)
		}

		return nil, fmt.Errorf("failed to get workflow from Tinkerbell: %w", err)
	}

	return tinkWorkflow, nil
}

// Create a Tinkerbell Workflow.
func (t *Workflow) Create(ctx context.Context, template, hardware string) (string, error) {
	req := &workflow.CreateRequest{
		Template: template,
		Hardware: hardware,
	}

	resp, err := t.client.CreateWorkflow(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create workflow in Tinkerbell: %w", err)
	}

	return resp.GetId(), nil
}

// Delete a Tinkerbell Workflow.
func (t *Workflow) Delete(ctx context.Context, id string) error {
	if _, err := t.client.DeleteWorkflow(ctx, &workflow.GetRequest{Id: id}); err != nil {
		if err.Error() == sqlErrorString {
			return fmt.Errorf("workflow %w", ErrNotFound)
		}

		return fmt.Errorf("failed to delete workflow from Tinkerbell: %w", err)
	}

	return nil
}
