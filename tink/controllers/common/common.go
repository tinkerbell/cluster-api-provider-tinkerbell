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

// Package common contains common controller logic for Tinkerbell controllers.
package common

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ErrNotImplemented is returned if a requested action is not yet implemented.
var ErrNotImplemented = errors.New("not implemented")

// EnsureFinalizer ensures the given finalizer is applied to the resource.
func EnsureFinalizer(
	ctx context.Context,
	c client.Client,
	logger logr.Logger,
	obj client.Object,
	finalizer string,
) error {
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))

	controllerutil.AddFinalizer(obj, finalizer)

	if err := c.Patch(ctx, obj, patch); err != nil {
		logger.Error(err, "Failed to add finalizer to resource")

		return fmt.Errorf("failed to add finalizer to resource: %w", err)
	}

	return nil
}
