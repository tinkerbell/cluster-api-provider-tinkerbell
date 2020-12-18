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

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrastructurev1alpha3 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
)

// TinkerbellMachineReconciler implements Reconciler interface by managing Tinkerbell machines.
type TinkerbellMachineReconciler struct {
	Log    logr.Logger
	Client client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch

// Reconcile ensures that all Tinkerbell machines are aligned with a given spec.
func (tmr *TinkerbellMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	bmrc, result, err := tmr.newReconcileContext(ctx, req.NamespacedName)
	if err != nil {
		return result, fmt.Errorf("creating reconciliation context: %w", err)
	}

	if bmrc == nil {
		return result, nil
	}

	if bmrc.MachineScheduledForDeletion() {
		return ctrl.Result{}, bmrc.DeleteMachineWithDependencies()
	}

	mrc, err := bmrc.IntoMachineReconcileContext()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building machine reconcile context: %w", err)
	}

	if mrc == nil {
		return defaultRequeueResult(), nil
	}

	return ctrl.Result{}, mrc.Reconcile()
}

// SetupWithManager configures reconciler with a given manager.
func (tmr *TinkerbellMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha3.TinkerbellMachine{}).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.MachineToInfrastructureMapFunc(infrastructurev1alpha3.GroupVersion.WithKind("TinkerbellMachine")),
			},
		).
		Complete(tmr)
}

// defaultRequeueResult returns requeue result with default requeue time defined, as Go does not support const structs.
func defaultRequeueResult() ctrl.Result {
	return ctrl.Result{
		// TODO: Why 5 seconds is a good value? Usually it takes few seconds for other controllers to converge
		// and prepare dependencies for Machine and Cluster objects, so 5 seconds wait time should provide good
		// balance, as sync time might be e.g. 10 minutes.
		RequeueAfter: 5 * time.Second, //nolint:gomnd
	}
}
