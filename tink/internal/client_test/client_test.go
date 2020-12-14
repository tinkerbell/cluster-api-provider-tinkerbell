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
	"errors"

	"github.com/tinkerbell/tink/protos/template"
	"google.golang.org/grpc"
)

type fakeClient struct {
	objs map[string]*template.WorkflowTemplate
}

func newFakeClient(objs ...*template.WorkflowTemplate) *fakeClient {
	f := &fakeClient{objs: map[string]*template.WorkflowTemplate{}}

	for i, obj := range objs {
		id := obj.GetId()

		if id == "" {
			obj.Id = obj.GetName()
		}

		f.objs[id] = objs[i]
	}

	return f
}

func (f *fakeClient) CreateTemplate(
	ctx context.Context,
	in *template.WorkflowTemplate,
	opts ...grpc.CallOption,
) (*template.CreateResponse, error) {
	id := in.GetId()

	if id == "" {
		id = in.GetName()
	}

	if _, ok := f.objs[id]; ok {
		return nil, errors.New("duplicate")
	}

	f.objs[id] = &template.WorkflowTemplate{
		Id:   id,
		Name: in.GetName(),
		Data: in.GetData(),
	}

	return &template.CreateResponse{Id: id}, nil
}

func (f *fakeClient) GetTemplate(
	ctx context.Context,
	in *template.GetRequest,
	opts ...grpc.CallOption,
) (*template.WorkflowTemplate, error) {
	id := in.GetId()
	if id == "" {
		id = in.GetName()
	}

	if _, ok := f.objs[id]; ok {
		return f.objs[id], nil
	}

	return nil, errors.New("rpc error: code = Unknown desc = sql: no rows in result set")
}

func (f *fakeClient) DeleteTemplate(
	ctx context.Context,
	in *template.GetRequest,
	opts ...grpc.CallOption,
) (*template.Empty, error) {
	id := in.GetId()
	if id == "" {
		id = in.GetName()
	}

	if _, ok := f.objs[id]; ok {
		delete(f.objs, id)

		return &template.Empty{}, nil
	}

	return nil, errors.New("rpc error: code = Unknown desc = sql: no rows in result set")
}

func (f *fakeClient) ListTemplates(
	ctx context.Context,
	in *template.ListRequest,
	opts ...grpc.CallOption,
) (template.TemplateService_ListTemplatesClient, error) {
	return nil, errors.New("nobody home")
}

func (f *fakeClient) UpdateTemplate(
	ctx context.Context,
	in *template.WorkflowTemplate,
	opts ...grpc.CallOption,
) (*template.Empty, error) {
	id := in.GetId()

	if id == "" {
		in.Id = in.GetName()
	}

	if _, ok := f.objs[id]; ok {
		f.objs[id].Data = in.GetData()

		return &template.Empty{}, nil
	}

	return nil, errors.New("nobody home")
}
