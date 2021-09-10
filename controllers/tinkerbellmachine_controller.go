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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha4"
)

// TinkerbellMachineReconciler implements Reconciler interface by managing Tinkerbell machines.
type TinkerbellMachineReconciler struct {
	client.Client
	WatchFilterValue string
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch

// Reconcile ensures that all Tinkerbell machines are aligned with a given spec.
func (tmr *TinkerbellMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	bmrc, result, err := tmr.newReconcileContext(ctx, req.NamespacedName)
	if err != nil {
		return result, fmt.Errorf("creating reconciliation context: %w", err)
	}

	if bmrc == nil {
		return result, nil
	}

	if bmrc.MachineScheduledForDeletion() {
		return ctrl.Result{}, bmrc.DeleteMachineWithDependencies() //nolint:wrapcheck
	}

	mrc, err := bmrc.IntoMachineReconcileContext()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building machine reconcile context: %w", err)
	}

	if mrc == nil {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, mrc.Reconcile() //nolint:wrapcheck
}

// SetupWithManager configures reconciler with a given manager.
func (tmr *TinkerbellMachineReconciler) SetupWithManager(
	ctx context.Context,
	mgr ctrl.Manager,
	options controller.Options,
) error {
	log := ctrl.LoggerFrom(ctx)

	clusterToObjectFunc, err := util.ClusterToObjectsMapper(
		tmr.Client,
		&infrastructurev1.TinkerbellMachineList{},
		mgr.GetScheme(),
	)
	if err != nil {
		return fmt.Errorf("failed to create mapper for Cluster to TinkrebellMachines: %w", err)
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(log, tmr.WatchFilterValue)).
		For(&infrastructurev1.TinkerbellMachine{}).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(
				util.MachineToInfrastructureMapFunc(infrastructurev1.GroupVersion.WithKind("TinkerbellMachine")),
			),
		).
		Watches(
			&source.Kind{Type: &infrastructurev1.TinkerbellCluster{}},
			handler.EnqueueRequestsFromMapFunc(tmr.TinkerbellClusterToTinkerbellMachines(ctx)),
		).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(clusterToObjectFunc),
			builder.WithPredicates(predicates.ClusterUnpausedAndInfrastructureReady(log)),
		)

	if err := builder.Complete(tmr); err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	return nil
}

// TinkerbellClusterToTinkerbellMachines is a handler.ToRequestsFunc to be used to enqeue requests for reconciliation
// of TinkerbellMachines.
func (tmr *TinkerbellMachineReconciler) TinkerbellClusterToTinkerbellMachines(ctx context.Context) handler.MapFunc {
	log := ctrl.LoggerFrom(ctx)

	return func(o client.Object) []ctrl.Request {
		c, ok := o.(*infrastructurev1.TinkerbellCluster)
		if !ok {
			log.Error(
				fmt.Errorf("expected a TinkerbellCluster but got a %T", o), //nolint:goerr113
				"failed to get TinkerbellMachine for TinkerbellCluster",
			)

			return nil
		}

		log = log.WithValues("TinkerbellCluster", c.Name, "Namespace", c.Namespace)

		// Don't handle deleted TinkerbellClusters
		if !c.ObjectMeta.DeletionTimestamp.IsZero() {
			log.V(4).Info("TinkerbellCluster has a deletion timestamp, skipping mapping.") //nolint:gomnd

			return nil
		}

		cluster, err := util.GetOwnerCluster(ctx, tmr.Client, c.ObjectMeta)

		switch {
		case apierrors.IsNotFound(err) || cluster == nil:
			log.Error(err, "owning cluster is not found, skipping mapping.")

			return nil
		case err != nil:
			log.Error(err, "failed to get owning cluster")

			return nil
		}

		machines, err := collections.GetFilteredMachinesForCluster(ctx, tmr.Client, cluster)
		if err != nil {
			log.Error(err, "failed to get Machines for Cluster")

			return nil
		}

		var result []ctrl.Request

		for _, m := range machines.UnsortedList() {
			if m.Spec.InfrastructureRef.Name == "" {
				continue
			}

			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.InfrastructureRef.Name}

			result = append(result, ctrl.Request{NamespacedName: name})
		}

		return result
	}
}
