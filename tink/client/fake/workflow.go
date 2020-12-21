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
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	"github.com/tinkerbell/tink/protos/workflow"
	"google.golang.org/protobuf/proto"
)

// Workflow is a fake client for Tinkerbell Workflows.
type Workflow struct {
	Objs           map[string]*workflow.Workflow
	hwClient       Hardware
	templateClient Template
}

// NewFakeWorkflowClient returns a new fake client.
func NewFakeWorkflowClient(hwClient Hardware, templateClient Template, objs ...*workflow.Workflow) *Workflow {
	f := &Workflow{
		Objs:           map[string]*workflow.Workflow{},
		hwClient:       hwClient,
		templateClient: templateClient,
	}

	for _, obj := range objs {
		if obj.GetId() == "" {
			obj.Id = uuid.New().String()
		}

		f.Objs[obj.Id] = proto.Clone(obj).(*workflow.Workflow)
	}

	return f
}

// Create creates a new Workflow.
func (f *Workflow) Create(ctx context.Context, templateID, hardwareID string) (string, error) {
	id := uuid.New().String()

	f.Objs[id] = &workflow.Workflow{
		Id:       id,
		Template: templateID,
		Hardware: hardwareID,
		// TODO: populate fake Data
	}

	return id, nil
}

// Get gets a Workflow from Tinkerbell.
func (f *Workflow) Get(ctx context.Context, id string) (*workflow.Workflow, error) {
	if _, ok := f.Objs[id]; ok {
		return proto.Clone(f.Objs[id]).(*workflow.Workflow), nil
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
