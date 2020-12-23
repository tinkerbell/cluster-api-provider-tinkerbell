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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	tinkclient "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/common"
	"github.com/tinkerbell/tink/protos/workflow"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type workflowClient interface {
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
	Create(ctx context.Context, templateID, hardwareID string) (string, error)
	Delete(ctx context.Context, id string) error
}

// Reconciler implements Reconciler interface by managing Tinkerbell workflows.
type Reconciler struct {
	client.Client
	WorkflowClient workflowClient
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, workflowChan <-chan event.GenericEvent) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Workflow{}).
		Watches(
			&source.Channel{Source: workflowChan},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=workflows;workflows/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell workflows.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("workflow", req.NamespacedName.Name)

	// Fetch the workflow.
	workflow := &tinkv1alpha1.Workflow{}
	if err := r.Get(ctx, req.NamespacedName, workflow); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get workflow")

		return ctrl.Result{}, fmt.Errorf("failed to get workflow: %w", err)
	}

	// Ensure that we add the finalizer to the resource
	if err := common.EnsureFinalizer(ctx, r.Client, logger, workflow, tinkv1alpha1.WorkflowFinalizer); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure finalizer on workflow: %w", err)
	}

	// Handle deleted wokflows.
	if !workflow.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, workflow)
	}

	return r.reconcileNormal(ctx, workflow)
}

func (r *Reconciler) reconcileNormal(ctx context.Context, w *tinkv1alpha1.Workflow) (ctrl.Result, error) {
	logger := r.Log.WithValues("workflow", w.Name)

	workflowID := w.TinkID()

	if workflowID == "" {
		id, err := r.createWorkflow(ctx, w)
		if err != nil {
			return ctrl.Result{}, err
		}

		workflowID = id
	}

	tinkWorkflow, err := r.WorkflowClient.Get(ctx, workflowID)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := common.EnsureAnnotation(ctx, r.Client, logger, w, tinkv1alpha1.WorkflowIDAnnotation,
		workflowID); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure id annotation on workflow: %w", err)
	}

	return r.reconcileStatus(ctx, w, tinkWorkflow)
}

func (r *Reconciler) reconcileStatus(ctx context.Context, w *tinkv1alpha1.Workflow, tinkWorkflow *workflow.Workflow) (ctrl.Result, error) {
	logger := r.Log.WithValues("workflow", w.Name)
	patch := client.MergeFrom(w.DeepCopy())

	w.Status.Data = tinkWorkflow.GetData()
	w.Status.State = tinkWorkflow.GetState().String()

	if err := r.Client.Status().Patch(ctx, w, patch); err != nil {
		logger.Error(err, "Failed to patch workflow")

		return ctrl.Result{}, fmt.Errorf("failed to patch workflow: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) createWorkflow(ctx context.Context, w *tinkv1alpha1.Workflow) (string, error) {
	logger := r.Log.WithValues("workflow", w.Name)

	hw := &tinkv1alpha1.Hardware{}
	hwKey := client.ObjectKey{Name: w.Spec.HardwareRef}

	if err := r.Client.Get(ctx, hwKey, hw); err != nil {
		logger.Error(err, "Failed to get hardware")

		return "", fmt.Errorf("failed to get hardware: %w", err)
	}

	t := &tinkv1alpha1.Template{}
	tKey := client.ObjectKey{Name: w.Spec.TemplateRef}

	if err := r.Client.Get(ctx, tKey, t); err != nil {
		logger.Error(err, "Failed to get template")

		return "", fmt.Errorf("failed to get template: %w", err)
	}

	id, err := r.WorkflowClient.Create(
		ctx, t.TinkID(),
		hw.Spec.ID,
	)
	if err != nil {
		logger.Error(err, "Failed to create workflow")

		return "", fmt.Errorf("failed to create workflow: %w", err)
	}

	return id, nil
}

func (r *Reconciler) reconcileDelete(ctx context.Context, w *tinkv1alpha1.Workflow) (ctrl.Result, error) {
	// Create a patch for use later
	patch := client.MergeFrom(w.DeepCopy())

	logger := r.Log.WithValues("workflow", w.Name)

	annotations := w.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	// If we've recorded an ID for the Workflow, then we should delete it
	if id, ok := annotations[tinkv1alpha1.WorkflowIDAnnotation]; ok {
		err := r.WorkflowClient.Delete(ctx, id)
		if err != nil && !errors.Is(err, tinkclient.ErrNotFound) {
			logger.Error(err, "Failed to delete workflow from Tinkerbell")

			return ctrl.Result{}, fmt.Errorf("failed to delete workflow from Tinkerbell: %w", err)
		}
	}

	controllerutil.RemoveFinalizer(w, tinkv1alpha1.WorkflowFinalizer)

	if err := r.Client.Patch(ctx, w, patch); err != nil {
		logger.Error(err, "Failed to patch workflow")

		return ctrl.Result{}, fmt.Errorf("failed to patch workflow: %w", err)
	}

	return ctrl.Result{}, nil
}
