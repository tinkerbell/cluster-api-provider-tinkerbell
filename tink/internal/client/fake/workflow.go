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

// Package fake contains a fake client wrapper for Tinkerbell.
package fake

import (
	"context"

	"github.com/google/uuid"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/internal/client"
	"github.com/tinkerbell/tink/protos/workflow"
)

// Workflow is a fake client for Tinkerbell Workflows.
type Workflow struct {
	Objs map[string]*workflow.Workflow
}

// NewFakeWorkflowClient returns a new fake client.
func NewFakeWorkflowClient(objs ...*workflow.Workflow) *Workflow {
	f := &Workflow{Objs: map[string]*workflow.Workflow{}}

	for i, obj := range objs {
		f.Objs[obj.GetId()] = objs[i]
	}

	return f
}

// Create creates a new Workflow.
func (f *Workflow) Create(ctx context.Context, template, hardware string) (string, error) {
	id := uuid.New().String()

	f.Objs[id] = &workflow.Workflow{
		Id:       id,
		Template: template,
		Hardware: hardware,
		// TODO: populate fake Data
	}

	return id, nil
}

// Get gets a Workflow from Tinkerbell.
func (f *Workflow) Get(ctx context.Context, id string) (*workflow.Workflow, error) {
	if _, ok := f.Objs[id]; ok {
		return f.Objs[id], nil
	}

	return nil, client.ErrNotFound
}

// Delete deletes a Workflow from Tinkerbell.
func (f *Workflow) Delete(ctx context.Context, id string) error {
	if _, ok := f.Objs[id]; ok {
		delete(f.Objs, id)

		return nil
	}

	return client.ErrNotFound
}
