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

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/internal/client"
	"github.com/tinkerbell/tink/protos/hardware"
)

// Hardware is a fake client for Tinkerbell Hardwares.
type Hardware struct {
	Objs map[string]*hardware.Hardware
}

// NewFakeHardwareClient returns a new fake client.
func NewFakeHardwareClient(objs ...*hardware.Hardware) *Hardware {
	f := &Hardware{Objs: map[string]*hardware.Hardware{}}

	for i, obj := range objs {
		f.Objs[obj.GetId()] = objs[i]
	}

	return f
}

// Get gets a Hardware from Tinkerbell.
func (f *Hardware) Get(ctx context.Context, id, mac, ip string) (*hardware.Hardware, error) {
	// TODO: need to implement fake ip and mac lookups
	if _, ok := f.Objs[id]; ok {
		return f.Objs[id], nil
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
