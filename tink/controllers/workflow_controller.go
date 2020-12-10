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
	"github.com/tinkerbell/tink/protos/workflow"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// WorkflowReconciler implements Reconciler interface by managing Tinkerbell workflows.
type WorkflowReconciler struct {
	client.Client
	WorkflowClient workflow.WorkflowServiceClient
	Log            logr.Logger
	Recorder       record.EventRecorder
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Workflow{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=workflows;workflows/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell workflows.
func (r *WorkflowReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("workflow", req.NamespacedName.Name)

	// Fetch the workflow.
	workflow := &tinkv1alpha1.Workflow{}
	if err := r.Get(ctx, req.NamespacedName, workflow); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("getting workflow: %w", err)
	}

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(workflow, tinkv1alpha1.WorkflowFinalizer) {
		before := workflow.DeepCopy()
		controllerutil.AddFinalizer(workflow, tinkv1alpha1.WorkflowFinalizer)

		if err := r.Client.Patch(ctx, workflow, client.MergeFrom(before)); err != nil {
			logger.Error(err, "Failed to add finalizer to workflow")

			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{}, nil
	}

	// Handle deleted wokflows.
	if !workflow.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(workflow)
	}

	return r.reconcileNormal(workflow)
}

func (r *WorkflowReconciler) reconcileNormal(workflow *tinkv1alpha1.Workflow) (ctrl.Result, error) {
	logger := r.Log.WithValues("workflow", workflow.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	return ctrl.Result{}, err
}

func (r *WorkflowReconciler) reconcileDelete(workflow *tinkv1alpha1.Workflow) (ctrl.Result, error) {
	logger := r.Log.WithValues("workflow", workflow.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	// controllerutil.RemoveFinalizer(workflow, tinkv1alpha1.WorkflowFinalizer)

	return ctrl.Result{}, err
}
