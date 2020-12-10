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

// Package template contains controller for Tinkerbell Templates.
package template

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	tinkclient "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/common"
	"github.com/tinkerbell/tink/protos/template"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type templateClient interface {
	Get(ctx context.Context, id, name string) (*template.WorkflowTemplate, error)
	Create(ctx context.Context, template *template.WorkflowTemplate) error
	Update(ctx context.Context, template *template.WorkflowTemplate) error
	Delete(ctx context.Context, id string) error
}

// Reconciler implements the Reconciler interface for managing Tinkerbell templates.
type Reconciler struct {
	client.Client
	TemplateClient templateClient
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, templateChan <-chan event.GenericEvent) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Template{}).
		Watches(
			&source.Channel{Source: templateChan},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=templates;templates/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell templates.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("template", req.NamespacedName.Name)

	// Fetch the template.
	template := &tinkv1alpha1.Template{}
	if err := r.Get(ctx, req.NamespacedName, template); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get template")

		return ctrl.Result{}, fmt.Errorf("failed to get template: %w", err)
	}

	// Ensure that we add the finalizer to the resource
	if err := common.EnsureFinalizer(ctx, r.Client, logger, template, tinkv1alpha1.TemplateFinalizer); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure finalizer on template: %w", err)
	}

	// Handle deleted templates.
	if !template.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, template)
	}

	return r.reconcileNormal(ctx, template)
}

func (r *Reconciler) reconcileNormal(ctx context.Context, t *tinkv1alpha1.Template) (ctrl.Result, error) {
	// Create a patch for use later
	logger := r.Log.WithValues("template", t.Name)

	templateID := t.TinkID()
	if templateID == "" {
		tinkTemplate, err := r.createTemplate(ctx, t)
		if err != nil {
			return ctrl.Result{}, err
		}

		templateID = tinkTemplate.Id
	}

	tinkTemplate, err := r.TemplateClient.Get(ctx, templateID, t.Name)

	switch {
	case errors.Is(err, tinkclient.ErrNotFound):
		result, err := r.createTemplate(ctx, t)
		if err != nil {
			return ctrl.Result{}, err
		}

		tinkTemplate = result
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("failed to get template: %w", err)
	}

	// Make sure that we record the tinkerbell id for the workflow
	patch := client.MergeFrom(t.DeepCopy())
	t.SetTinkID(templateID)

	if err := r.Client.Patch(ctx, t, patch); err != nil {
		logger.Error(err, "Failed to patch template")

		return ctrl.Result{}, fmt.Errorf("failed to patch template: %w", err)
	}

	// If the data is specified and different than what is stored in Tinkerbell,
	// update the template in Tinkerbell
	if t.Spec.Data != nil && *t.Spec.Data != tinkTemplate.GetData() {
		tinkTemplate.Data = *t.Spec.Data
		if err := r.TemplateClient.Update(ctx, tinkTemplate); err != nil {
			logger.Error(err, "Failed to update template in Tinkerbell")

			return ctrl.Result{}, fmt.Errorf("failed to update template in Tinkerbell: %w", err)
		}
	}

	patch = client.MergeFrom(t.DeepCopy())
	// If data was not specified, we are importing a pre-existing resource and should
	// populate it from Tinkerbell
	if t.Spec.Data == nil {
		t.Spec.Data = pointer.StringPtr(tinkTemplate.GetData())
	}

	if err := r.Client.Patch(ctx, t, patch); err != nil {
		logger.Error(err, "Failed to patch template")

		return ctrl.Result{}, fmt.Errorf("failed to patch template: %w", err)
	}

	return r.reconcileStatus(ctx, t, tinkTemplate)
}

func (r *Reconciler) reconcileStatus(ctx context.Context, t *tinkv1alpha1.Template, tinkTemplate *template.WorkflowTemplate) (ctrl.Result, error) {
	logger := r.Log.WithValues("template", t.Name)
	patch := client.MergeFrom(t.DeepCopy())

	t.Status.State = tinkv1alpha1.TemplateReady

	if err := r.Client.Status().Patch(ctx, t, patch); err != nil {
		logger.Error(err, "Failed to patch template")

		return ctrl.Result{}, fmt.Errorf("failed to patch template: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) createTemplate(ctx context.Context, t *tinkv1alpha1.Template) (*template.WorkflowTemplate, error) {
	logger := r.Log.WithValues("template", t.Name)

	tinkTemplate := &template.WorkflowTemplate{
		Name: t.Name,
		Data: pointer.StringPtrDerefOr(t.Spec.Data, ""),
	}

	if err := r.TemplateClient.Create(ctx, tinkTemplate); err != nil {
		logger.Error(err, "Failed to create template in Tinkerbell")

		return nil, fmt.Errorf("failed to create template in Tinkerbell: %w", err)
	}

	return tinkTemplate, nil
}

func (r *Reconciler) reconcileDelete(ctx context.Context, t *tinkv1alpha1.Template) (ctrl.Result, error) {
	// Create a patch for use later
	patch := client.MergeFrom(t.DeepCopy())

	logger := r.Log.WithValues("template", t.Name)

	// If we've recorded an ID for the Template, then we should delete it
	if id := t.TinkID(); id != "" {
		err := r.TemplateClient.Delete(ctx, id)
		if err != nil && !errors.Is(err, tinkclient.ErrNotFound) {
			logger.Error(err, "Failed to delete template from Tinkerbell")

			return ctrl.Result{}, fmt.Errorf("failed to delete template from Tinkerbell: %w", err)
		}
	}

	controllerutil.RemoveFinalizer(t, tinkv1alpha1.TemplateFinalizer)

	if err := r.Client.Patch(ctx, t, patch); err != nil {
		logger.Error(err, "Failed to patch template")

		return ctrl.Result{}, fmt.Errorf("failed to patch template: %w", err)
	}

	return ctrl.Result{}, nil
}
