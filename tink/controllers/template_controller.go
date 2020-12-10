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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var ErrNotImplemented = fmt.Errorf("not implemented")

type TemplateReconciler struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=templates,verbs=get;list;watch;create;update;patch;delete
func (r *TemplateReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("template", req.NamespacedName.Name)

	// Fetch the template.
	template := &tinkv1alpha1.Template{}
	if err := r.Get(ctx, req.NamespacedName, template); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("getting template: %w", err)
	}

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(template, tinkv1alpha1.TemplateFinalizer) {
		before := template.DeepCopy()
		controllerutil.AddFinalizer(template, tinkv1alpha1.TemplateFinalizer)

		if err := r.Client.Patch(ctx, template, client.MergeFrom(before)); err != nil {
			logger.Error(err, "Failed to add finalizer to template")

			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}

		return ctrl.Result{}, nil
	}

	// Handle deleted clusters.
	if !template.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(template)
	}

	return r.reconcileNormal(template)
}

func (r *TemplateReconciler) reconcileNormal(template *tinkv1alpha1.Template) (ctrl.Result, error) {
	logger := r.Log.WithValues("template", template.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	return ctrl.Result{}, err
}

func (r *TemplateReconciler) reconcileDelete(template *tinkv1alpha1.Template) (ctrl.Result, error) {
	logger := r.Log.WithValues("template", template.Name)
	err := ErrNotImplemented

	logger.Error(err, "Not yet implemented")

	// controllerutil.RemoveFinalizer(template, tinkv1alpha1.TemplateFinalizer)

	return ctrl.Result{}, err
}

func (r *TemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Template{}).
		Complete(r)
}
