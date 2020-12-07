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
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
	infrastructurev1alpha3 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
)

type TinkerbellClusterReconciler struct {
	client.Client
	Log      logr.Logger
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

func (r *TinkerbellClusterReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.Background()
	logger := r.Log.WithValues("tinkerbellcluster", req.NamespacedName)

	// your logic here
	tcluster := &infrastructurev1alpha3.TinkerbellCluster{}
	if err := r.Get(ctx, req.NamespacedName, tcluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger = logger.WithName(tcluster.APIVersion)

	// Fetch the Machine.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, tcluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster == nil {
		logger.Info("OwnerCluster is not set yet. Requeuing...")
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 2 * time.Second,
		}, nil
	}

	if util.IsPaused(cluster, tcluster) {
		logger.Info("TinkerbellCluster or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	// Handle deleted clusters
	if !cluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete()
	}

	return r.reconcileNormal(tcluster)
}

func (r *TinkerbellClusterReconciler) reconcileNormal(tcluster *v1alpha3.TinkerbellCluster) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *TinkerbellClusterReconciler) reconcileDelete() (ctrl.Result, error) {
	// Initially I created this handler to remove an elastic IP when a cluster
	// gets delete, but it does not sound like a good idea.  It is better to
	// leave to the users the ability to decide if they want to keep and resign
	// the IP or if they do not need it anymore
	return ctrl.Result{}, nil
}

func (r *TinkerbellClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha3.TinkerbellCluster{}).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.ClusterToInfrastructureMapFunc(infrastructurev1alpha3.GroupVersion.WithKind("TinkerbellCluster")),
			},
		).
		Complete(r)
}

// MachineNotFound error representing that the requested device was not yet found
type MachineNotFound struct {
	err string
}

func (e *MachineNotFound) Error() string {
	return e.err
}

// MachineNoIP error representing that the requested device does not have an IP yet assigned
type MachineNoIP struct {
	err string
}

func (e *MachineNoIP) Error() string {
	return e.err
}
