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
	"github.com/tinkerbell/tink/protos/hardware"
	"google.golang.org/protobuf/proto"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
)

// Hardware is a fake client for Tinkerbell Hardwares.
type Hardware struct {
	Objs map[string]*hardware.Hardware
}

// NewFakeHardwareClient returns a new fake client.
func NewFakeHardwareClient(objs ...*hardware.Hardware) *Hardware {
	f := &Hardware{Objs: map[string]*hardware.Hardware{}}

	for _, obj := range objs {
		if obj.GetId() == "" {
			obj.Id = uuid.New().String()
		}

		f.Objs[obj.Id], _ = proto.Clone(obj).(*hardware.Hardware)
	}

	return f
}

// Create creates a new Hardware.
func (f *Hardware) Create(ctx context.Context, in *hardware.Hardware) error {
	if in.GetId() == "" {
		in.Id = uuid.New().String()
	}

	if _, ok := f.Objs[in.Id]; ok {
		return errors.New("duplicate")
	}

	f.Objs[in.Id], _ = proto.Clone(in).(*hardware.Hardware)

	return nil
}

// Update Hardware in Tinkerbell.
func (f *Hardware) Update(ctx context.Context, in *hardware.Hardware) error {
	if _, ok := f.Objs[in.Id]; ok {
		f.Objs[in.Id], _ = proto.Clone(in).(*hardware.Hardware)

		return nil
	}

	return errors.New("nobody home")
}

// Get gets a Hardware from Tinkerbell.
func (f *Hardware) Get(ctx context.Context, id, ip, mac string) (*hardware.Hardware, error) {
	switch {
	case id != "":
		if _, ok := f.Objs[id]; ok {
			return proto.Clone(f.Objs[id]).(*hardware.Hardware), nil
		}
	default:
		for _, hw := range f.Objs {
			for _, i := range hw.GetNetwork().GetInterfaces() {
				switch {
				case mac != "":
					if i.GetDhcp().GetMac() == mac {
						return proto.Clone(hw).(*hardware.Hardware), nil
					}
				case ip != "":
					if i.GetDhcp().GetIp().Address == ip {
						return proto.Clone(hw).(*hardware.Hardware), nil
					}
				}
			}
		}
	}

	return nil, client.ErrNotFound
}

// Delete deletes a Hardware from Tinkerbell.
func (f *Hardware) Delete(ctx context.Context, id string) error {
	if _, ok := f.Objs[id]; ok {
		delete(f.Objs, id)

		return nil
	}

	return client.ErrNotFound
}
