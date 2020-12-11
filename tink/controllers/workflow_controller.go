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

		logger.Error(err, "Failed to get workflow")
		return ctrl.Result{}, err
	}

	// Ensure that we add the finalizer to the resource
	err := ensureFinalizer(ctx, r.Client, logger, workflow, tinkv1alpha1.WorkflowFinalizer)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Handle deleted wokflows.
	if !workflow.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, workflow)
	}

	return r.reconcileNormal(workflow)
}

func (r *WorkflowReconciler) reconcileNormal(workflow *tinkv1alpha1.Workflow) (ctrl.Result, error) {
	logger := r.Log.WithValues("workflow", workflow.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	return ctrl.Result{}, err
}

func (r *WorkflowReconciler) reconcileDelete(ctx context.Context, w *tinkv1alpha1.Workflow) (ctrl.Result, error) {
	// Create a patch for use later
	patch := client.MergeFrom(w.DeepCopy())

	logger := r.Log.WithValues("workflow", w.Name)

	if w.Annotations == nil {
		w.Annotations = map[string]string{}
	}

	id, ok := w.Annotations[tinkv1alpha1.WorkflowIDAnnotation]
	if !ok {
		// TODO: figure out how to lookup a workflow without an ID
		logger.Error(ErrNotImplemented, "Unable to delete a workflow without having recorded the ID")
		return ctrl.Result{}, ErrNotImplemented
	}

	workflowRequest := &workflow.GetRequest{
		Id: id,
	}

	_, err := r.WorkflowClient.DeleteWorkflow(ctx, workflowRequest)
	// TODO: Tinkerbell should return some type of status that is easier to handle
	// than parsing for this specific error message.
	if err != nil && err.Error() != "rpc error: code = Unknown desc = sql: no rows in result set" {
		logger.Error(err, "Failed to delete workflow from Tinkerbell")
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(w, tinkv1alpha1.WorkflowFinalizer)
	if err := r.Client.Patch(ctx, w, patch); err != nil {
		logger.Error(err, "Failed to patch workflow")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
