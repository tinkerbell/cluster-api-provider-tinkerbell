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
	"errors"

	"github.com/google/uuid"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/internal/client"
	"github.com/tinkerbell/tink/protos/template"
	"google.golang.org/protobuf/proto"
)

// Template is a fake client for Tinkerbell Templates.
type Template struct {
	Objs map[string]*template.WorkflowTemplate
}

// NewFakeTemplateClient returns a new fake client.
func NewFakeTemplateClient(objs ...*template.WorkflowTemplate) *Template {
	f := &Template{Objs: map[string]*template.WorkflowTemplate{}}

	for _, obj := range objs {
		if obj.GetId() == "" {
			obj.Id = uuid.New().String()
		}

		f.Objs[obj.Id] = proto.Clone(obj).(*template.WorkflowTemplate)
	}

	return f
}

// Create creates a new Template.
func (f *Template) Create(ctx context.Context, in *template.WorkflowTemplate) error {
	if in.GetId() == "" {
		in.Id = uuid.New().String()
	}

	if _, ok := f.Objs[in.Id]; ok {
		return errors.New("duplicate")
	}

	f.Objs[in.Id] = proto.Clone(in).(*template.WorkflowTemplate)

	return nil
}

// Get gets a Template from Tinkerbell.
func (f *Template) Get(ctx context.Context, id, name string) (*template.WorkflowTemplate, error) {
	switch {
	case id != "":
		if _, ok := f.Objs[id]; ok {
			return proto.Clone(f.Objs[id]).(*template.WorkflowTemplate), nil
		}
	default:
		for _, obj := range f.Objs {
			if obj.GetName() == name {
				return proto.Clone(obj).(*template.WorkflowTemplate), nil
			}
		}
	}

	return nil, client.ErrNotFound
}

// Delete deletes a Template from Tinkerbell.
func (f *Template) Delete(ctx context.Context, id string) error {
	if _, ok := f.Objs[id]; ok {
		delete(f.Objs, id)

		return nil
	}

	return client.ErrNotFound
}

// Update updates a Template from Tinkerbell.
func (f *Template) Update(ctx context.Context, in *template.WorkflowTemplate) error {
	if _, ok := f.Objs[in.Id]; ok {
		f.Objs[in.Id] = proto.Clone(in).(*template.WorkflowTemplate)

		return nil
	}

	return errors.New("nobody home")
}
