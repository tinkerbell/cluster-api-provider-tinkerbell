/*
Copyright 2022 The Tinkerbell Authors.

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

// Package workflow contains controllers for Tinkerbell workflow.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tinkerbell/tink/protos/workflow"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	tinkclient "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/common"
)

type workflowClient interface {
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
	Create(ctx context.Context, templateID, hardwareID string) (string, error)
	Delete(ctx context.Context, id string) error
	GetMetadata(ctx context.Context, id string) ([]byte, error)
	GetActions(ctx context.Context, id string) ([]*workflow.WorkflowAction, error)
	GetEvents(ctx context.Context, id string) ([]*workflow.WorkflowActionStatus, error)
	GetState(ctx context.Context, id string) (workflow.State, error)
}

// Reconciler implements Reconciler interface by managing Tinkerbell workflows.
type Reconciler struct {
	client.Client
	WorkflowClient workflowClient
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr). //nolint:wrapcheck
							WithOptions(options).
							For(&tinkv1alpha1.Workflow{}).
							Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=workflows;workflows/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell workflows.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", req.NamespacedName.Name)

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
	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", w.Name)

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
		return ctrl.Result{}, fmt.Errorf("failed to get workflow: %w", err)
	}

	// Make sure that we record the tinkerbell id for the workflow
	patch := client.MergeFrom(w.DeepCopy())
	w.SetTinkID(workflowID)

	if err := r.Client.Patch(ctx, w, patch); err != nil {
		logger.Error(err, "Failed to patch workflow")

		return ctrl.Result{}, fmt.Errorf("failed to patch workflow: %w", err)
	}

	return r.reconcileStatus(ctx, w, tinkWorkflow)
}

func actionFromTinkAction(action *workflow.WorkflowAction) tinkv1alpha1.Action {
	return tinkv1alpha1.Action{
		Name:        action.GetName(),
		TaskName:    action.GetTaskName(),
		Image:       action.GetImage(),
		Timeout:     action.GetTimeout(),
		Command:     action.GetCommand(),
		OnTimeout:   action.GetOnTimeout(),
		OnFailure:   action.GetOnFailure(),
		WorkerID:    action.GetWorkerId(),
		Volumes:     action.GetVolumes(),
		Environment: action.GetEnvironment(),
	}
}

func eventFromTinkEvent(event *workflow.WorkflowActionStatus) tinkv1alpha1.Event {
	return tinkv1alpha1.Event{
		ActionName:   event.GetActionName(),
		TaskName:     event.GetTaskName(),
		ActionStatus: event.GetActionStatus().String(),
		Seconds:      event.GetSeconds(),
		Message:      event.GetMessage(),
		WorkerID:     event.GetWorkerId(),
		CreatedAt:    metav1.NewTime(event.GetCreatedAt().AsTime()),
	}
}

func (r *Reconciler) reconcileStatusEvents(ctx context.Context, w *tinkv1alpha1.Workflow, id string) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", w.Name)

	events, err := r.WorkflowClient.GetEvents(ctx, id)
	if err != nil {
		logger.Error(err, "Failed to get events for workflow")

		return fmt.Errorf("failed to get events for workflow: %w", err)
	}

	statusEvents := make([]tinkv1alpha1.Event, 0, len(events))
	for _, event := range events {
		statusEvents = append(statusEvents, eventFromTinkEvent(event))
	}

	w.Status.Events = statusEvents

	return nil
}

func (r *Reconciler) reconcileStatusActions(ctx context.Context, w *tinkv1alpha1.Workflow, id string) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", w.Name)

	actions, err := r.WorkflowClient.GetActions(ctx, id)
	if err != nil {
		logger.Error(err, "Failed to get actions for workflow")

		return fmt.Errorf("failed to get actions for workflow: %w", err)
	}

	statusActions := make([]tinkv1alpha1.Action, 0, len(actions))
	for _, action := range actions {
		statusActions = append(statusActions, actionFromTinkAction(action))
	}

	w.Status.Actions = statusActions

	return nil
}

func (r *Reconciler) reconcileStatus(
	ctx context.Context,
	w *tinkv1alpha1.Workflow,
	tinkWorkflow *workflow.Workflow,
) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", w.Name)
	patch := client.MergeFrom(w.DeepCopy())

	// Try to patch what we have even if we hit errors gather additional status information
	defer func() {
		if err := r.Client.Status().Patch(ctx, w, patch); err != nil {
			logger.Error(err, "Failed to patch workflow status")
		}
	}()

	w.Status.Data = tinkWorkflow.GetData()

	md, err := r.WorkflowClient.GetMetadata(ctx, tinkWorkflow.GetId())
	if err != nil {
		logger.Error(err, "Failed to get metadata for workflow")

		return ctrl.Result{}, fmt.Errorf("failed to get metadata for workflow: %w", err)
	}

	w.Status.Metadata = string(md)

	if err := r.reconcileStatusActions(ctx, w, tinkWorkflow.GetId()); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileStatusEvents(ctx, w, tinkWorkflow.GetId()); err != nil {
		return ctrl.Result{}, err
	}

	state, err := r.WorkflowClient.GetState(ctx, tinkWorkflow.GetId())
	if err != nil {
		logger.Error(err, "Failed to get state for workflow")

		return ctrl.Result{}, fmt.Errorf("failed to get state for workflow: %w", err)
	}

	w.Status.State = state.String()

	if state != workflow.State_STATE_SUCCESS {
		// If the workflow hasn't successfully run, requeue in
		// a minute. This is to workaround the lack of events
		// for workflow status
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) createWorkflow(ctx context.Context, w *tinkv1alpha1.Workflow) (string, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", w.Name)

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

	logger := ctrl.LoggerFrom(ctx).WithValues("workflow", w.Name)

	// If we've recorded an ID for the Workflow, then we should delete it
	if id := w.TinkID(); id != "" {
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
