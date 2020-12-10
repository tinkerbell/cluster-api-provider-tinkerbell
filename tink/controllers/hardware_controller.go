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

// Package controllers contains controllers for Tinkerbell.
package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	"github.com/tinkerbell/tink/protos/hardware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// HardwareReconciler implements Reconciler interface by managing Tinkerbell hardware.
type HardwareReconciler struct {
	client.Client
	HardwareClient hardware.HardwareServiceClient
	Log            logr.Logger
	Recorder       record.EventRecorder
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *HardwareReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Hardware{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=hardware;hardware/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell hardware.
func (r *HardwareReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("hardware", req.NamespacedName.Name)

	// Fetch the hardware.
	hardware := &tinkv1alpha1.Hardware{}
	if err := r.Get(ctx, req.NamespacedName, hardware); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("getting hardware: %w", err)
	}

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(hardware, tinkv1alpha1.HardwareFinalizer) {
		before := hardware.DeepCopy()
		controllerutil.AddFinalizer(hardware, tinkv1alpha1.HardwareFinalizer)

		if err := r.Client.Patch(ctx, hardware, client.MergeFrom(before)); err != nil {
			logger.Error(err, "Failed to add finalizer to hardware")

			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{}, nil
	}

	// Handle deleted hardware.
	if !hardware.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(hardware)
	}

	return r.reconcileNormal(hardware)
}

func (r *HardwareReconciler) reconcileNormal(hardware *tinkv1alpha1.Hardware) (ctrl.Result, error) {
	logger := r.Log.WithValues("hardware", hardware.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	return ctrl.Result{}, err
}

func (r *HardwareReconciler) reconcileDelete(hardware *tinkv1alpha1.Hardware) (ctrl.Result, error) {
	logger := r.Log.WithValues("hardware", hardware.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	// controllerutil.RemoveFinalizer(hardware, tinkv1alpha1.HardwareFinalizer)

	return ctrl.Result{}, err
}
