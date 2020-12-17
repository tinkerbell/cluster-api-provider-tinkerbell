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

// Package hardware contains controller for Tinkerbell Hardware.
package hardware

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	"github.com/tinkerbell/tink/protos/hardware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type hardwareClient interface {
	// Create(ctx context.Context, h *hardware.Hardware) error
	// Update(ctx context.Context, h *hardware.Hardware) error
	Get(ctx context.Context, id, ip, mac string) (*hardware.Hardware, error)
	// Delete(ctx context.Context, id string) error
}

// Reconciler implements Reconciler interface by managing Tinkerbell hardware.
type Reconciler struct {
	client.Client
	HardwareClient hardwareClient
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Hardware{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=hardware;hardware/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell hardware.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("hardware", req.NamespacedName.Name)

	// Fetch the hardware.
	hardware := &tinkv1alpha1.Hardware{}
	if err := r.Get(ctx, req.NamespacedName, hardware); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get hardware")

		return ctrl.Result{}, fmt.Errorf("failed to get hardware: %w", err)
	}

	// Deletion is a noop.
	if !hardware.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.reconcileNormal(ctx, hardware)
}

func (r *Reconciler) reconcileNormal(ctx context.Context, h *tinkv1alpha1.Hardware) (ctrl.Result, error) {
	logger := r.Log.WithValues("hardware", h.Name)

	tinkHardware, err := r.HardwareClient.Get(ctx, h.Spec.ID, "", "")
	if err != nil {
		logger.Error(err, "Failed to get hardware from Tinkerbell")

		return ctrl.Result{}, fmt.Errorf("failed to get hardware from Tinkerbell: %w", err)
	}

	logger.Info("Found hardware in tinkerbell", "tinkHardware", tinkHardware)

	return ctrl.Result{}, nil
}
