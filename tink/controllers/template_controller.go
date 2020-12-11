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
	"github.com/tinkerbell/tink/protos/template"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var ErrNotImplemented = fmt.Errorf("not implemented")

// TemplateReconciler implements Reconciler interface by managing Tinkerbell templates.
type TemplateReconciler struct {
	client.Client
	TemplateClient template.TemplateServiceClient
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *TemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Template{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=templates;templates/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell templates.
func (r *TemplateReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("template", req.NamespacedName.Name)

	// Fetch the template.
	template := &tinkv1alpha1.Template{}
	if err := r.Get(ctx, req.NamespacedName, template); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get template")
		return ctrl.Result{}, err
	}

	// Ensure that we add the finalizer to the resource
	err := ensureFinalizer(ctx, r.Client, logger, template, tinkv1alpha1.TemplateFinalizer)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Handle deleted templates.
	if !template.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, template)
	}

	return r.reconcileNormal(ctx, template)
}

func (r *TemplateReconciler) reconcileNormal(ctx context.Context, t *tinkv1alpha1.Template) (ctrl.Result, error) {
	// Create a patch for use later
	patch := client.MergeFrom(t.DeepCopy())

	logger := r.Log.WithValues("template", t.Name)

	templateRequest := &template.GetRequest{}

	if t.Annotations == nil {
		t.Annotations = map[string]string{}
	}

	if id, ok := t.Annotations[tinkv1alpha1.TemplateIDAnnotation]; ok && id != "" {
		// We've already recorded the ID, so we should prefer to use that.
		templateRequest.GetBy = &template.GetRequest_Id{Id: id}
	} else {
		// We don't have an ID, so try to get the template by name.
		// TODO: this will need to be improved for multitenancy support.
		templateRequest.GetBy = &template.GetRequest_Name{Name: t.Name}
	}

	tinkTemplate, err := r.TemplateClient.GetTemplate(ctx, templateRequest)
	if err != nil {
		// TODO: Tinkerbell should return some type of status that is easier to handle
		// than parsing for this specific error message.
		if err.Error() != "rpc error: code = Unknown desc = sql: no rows in result set" {
			logger.Error(err, "Failed to get template from Tinkerbell")
			return ctrl.Result{}, err
		}

		// We should create the template
		tinkTemplate = &template.WorkflowTemplate{
			Name: t.Name,
			Data: pointer.StringPtrDerefOr(t.Spec.Data, ""),
		}
		resp, err := r.TemplateClient.CreateTemplate(ctx, tinkTemplate)
		if err != nil {
			logger.Error(err, "Failed to create template in Tinkerbell")
			return ctrl.Result{}, err
		}
		tinkTemplate.Id = resp.GetId()
	}

	// If the data is specified and different than what is stored in Tinkerbell,
	// update the template in Tinkerbell
	if t.Spec.Data != nil && *t.Spec.Data != tinkTemplate.GetData() {
		tinkTemplate.Data = *t.Spec.Data
		if _, err := r.TemplateClient.UpdateTemplate(ctx, tinkTemplate); err != nil {
			logger.Error(err, "Failed to update template in Tinkerbell")
			return ctrl.Result{}, err
		}
	}

	// Ensure that we populate the ID
	t.Annotations[tinkv1alpha1.TemplateIDAnnotation] = tinkTemplate.Id

	// If data was not specified, we are importing a pre-existing resource and should
	// populate it from Tinkerbell
	if t.Spec.Data == nil {
		t.Spec.Data = pointer.StringPtr(tinkTemplate.GetData())
	}

	if err := r.Client.Patch(ctx, t, patch); err != nil {
		logger.Error(err, "Failed to patch template")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TemplateReconciler) reconcileDelete(ctx context.Context, t *tinkv1alpha1.Template) (ctrl.Result, error) {
	// Create a patch for use later
	patch := client.MergeFrom(t.DeepCopy())

	logger := r.Log.WithValues("template", t.Name)

	if t.Annotations == nil {
		t.Annotations = map[string]string{}
	}

	templateRequest := &template.GetRequest{}

	if id, ok := t.Annotations[tinkv1alpha1.TemplateIDAnnotation]; ok && id != "" {
		// We've already recorded the ID, so we should prefer to use that.
		templateRequest.GetBy = &template.GetRequest_Id{Id: id}
	} else {
		// We don't have an ID, so try to get the template by name.
		// TODO: this will need to be improved for multitenancy support.
		templateRequest.GetBy = &template.GetRequest_Name{Name: t.Name}
	}

	_, err := r.TemplateClient.DeleteTemplate(ctx, templateRequest)
	// TODO: Tinkerbell should return some type of status that is easier to handle
	// than parsing for this specific error message.
	if err != nil && err.Error() != "rpc error: code = Unknown desc = sql: no rows in result set" {
		logger.Error(err, "Failed to delete template from Tinkerbell")
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(t, tinkv1alpha1.TemplateFinalizer)
	if err := r.Client.Patch(ctx, t, patch); err != nil {
		logger.Error(err, "Failed to patch template")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
