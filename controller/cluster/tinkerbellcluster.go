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

// Package cluster contains Cluster API controller for the TinkerbellCluster CR.
package cluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/paused"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

const (
	// ClusterNameLabel is used to mark Hardware as assigned controlplane machine.
	ClusterNameLabel = "v1alpha1.tinkerbell.org/clusterName"

	// ClusterNamespaceLabel is used to mark in which Namespace hardware is used.
	ClusterNamespaceLabel = "v1alpha1.tinkerbell.org/clusterNamespace"

	// KubernetesAPIPort is a port used by Tinkerbell clusters for Kubernetes API.
	KubernetesAPIPort = 6443

	// failureDomainResyncInterval is how often a cluster with Spec.FailureDomainLabel set is
	// re-reconciled so status.failureDomains picks up Hardware that was added, removed or
	// relabelled.
	//
	// This is only used in external Tinkerbell mode (--external-kubeconfig), where Hardware
	// lives in another cluster that the manager's cache cannot watch. In the default local
	// mode the controller watches Hardware directly and this interval is not used.
	failureDomainResyncInterval = 5 * time.Minute
)

var (
	// ErrClusterNotReady is returned when trying to reconcile prior to the Cluster resource being ready.
	ErrClusterNotReady = fmt.Errorf("cluster resource not ready")
	// ErrControlPlaneEndpointNotSet is returned when trying to reconcile when the ControlPlane Endpoint is not defined.
	ErrControlPlaneEndpointNotSet = fmt.Errorf("controlplane endpoint is not set")
	// ErrConfigurationNil is the error returned when TinkerbellMachineReconciler or TinkerbellClusterReconciler is nil.
	ErrConfigurationNil = fmt.Errorf("configuration is nil")
	// ErrMissingClient is the error returned when TinkerbellMachineReconciler or TinkerbellClusterReconciler do
	// not have a Client configured.
	ErrMissingClient = fmt.Errorf("client is nil")
)

// TinkerbellClusterReconciler implements Reconciler interface.
type TinkerbellClusterReconciler struct {
	client.Client
	// TinkerbellClient reads Tinkerbell CRDs. In local mode it is the same client as
	// Client; in external mode it targets a separate Tinkerbell cluster. When nil, the
	// management cluster client is used.
	TinkerbellClient client.Client
	// ExternalTinkerbell is true when TinkerbellClient targets a separate Tinkerbell
	// cluster, whose Hardware the manager's cache cannot watch.
	ExternalTinkerbell bool
	WatchFilterValue   string
}

// validate validates if context configuration has all required fields properly populated.
func (tcr *TinkerbellClusterReconciler) validate() error {
	if tcr == nil {
		return ErrConfigurationNil
	}

	if tcr.Client == nil {
		return ErrMissingClient
	}

	return nil
}

// New builds a context for cluster reconciliation process, collecting all required
// information.
//
// If unexpected case occurs, error is returned.
//
// If some data is not yet available, nil is returned.
func (tcr *TinkerbellClusterReconciler) newReconcileContext(ctx context.Context, namespacedName types.NamespacedName) (*clusterReconcileContext, error) {
	log := ctrl.LoggerFrom(ctx)

	if err := tcr.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	tinkerbellClient := tcr.TinkerbellClient
	if tinkerbellClient == nil {
		tinkerbellClient = tcr.Client
	}

	crc := &clusterReconcileContext{
		log:                log.WithValues("tinkerbellcluster", namespacedName),
		ctx:                ctx,
		tinkerbellCluster:  &infrastructurev1.TinkerbellCluster{},
		client:             tcr.Client,
		tinkerbellClient:   tinkerbellClient,
		externalTinkerbell: tcr.ExternalTinkerbell,
		namespacedName:     namespacedName,
	}

	if err := crc.client.Get(crc.ctx, namespacedName, crc.tinkerbellCluster); err != nil {
		if apierrors.IsNotFound(err) {
			crc.log.Info("TinkerbellCluster object not found")

			return nil, nil
		}

		return nil, fmt.Errorf("getting TinkerbellCluster: %w", err)
	}

	patchHelper, err := patch.NewHelper(crc.tinkerbellCluster, crc.client)
	if err != nil {
		return nil, fmt.Errorf("initializing patch helper: %w", err)
	}

	crc.patchHelper = patchHelper

	cluster, err := util.GetOwnerCluster(crc.ctx, crc.client, crc.tinkerbellCluster.ObjectMeta)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("getting owner cluster: %w", err)
		}
	}

	if cluster == nil {
		crc.log.Info("OwnerCluster is not set yet; trying by label")
		cluster, err = findClusterOwnerByLabelAndAdopt(crc)
		if err != nil {
			return nil, fmt.Errorf("getting by-label owner cluster: %w", err)
		}
	}

	crc.cluster = cluster

	return crc, nil
}

func findClusterOwnerByLabelAndAdopt(crc *clusterReconcileContext) (*clusterv1.Cluster, error) {
	clusterByLabel, err := util.GetClusterFromMetadata(crc.ctx, crc.client, crc.tinkerbellCluster.ObjectMeta)
	if err == nil && clusterByLabel != nil {
		crc.log.Info("Cluster found by label, setting as owner reference")
		// "Set" the owner reference so that future reconciliations don't have to do this again.
		if err := ctrl.SetControllerReference(clusterByLabel, crc.tinkerbellCluster, crc.client.Scheme()); err != nil {
			return nil, fmt.Errorf("setting owner reference: %w", err)
		}
		if err := crc.patchHelper.Patch(crc.ctx, crc.tinkerbellCluster); err != nil {
			return nil, fmt.Errorf("patching TinkerbellCluster with owner reference: %w", err)
		}
		crc.log.Info("Owner reference set via label lookup")
	}
	return clusterByLabel, nil
}

// clusterReconcileContext implements ReconcileContext by reconciling TinkerbellCluster object.
type clusterReconcileContext struct {
	ctx                context.Context
	tinkerbellCluster  *infrastructurev1.TinkerbellCluster
	patchHelper        *patch.Helper
	cluster            *clusterv1.Cluster
	log                logr.Logger
	client             client.Client
	tinkerbellClient   client.Client
	externalTinkerbell bool
	namespacedName     types.NamespacedName
}

func (crc *clusterReconcileContext) controlPlaneEndpoint() (clusterv1.APIEndpoint, error) {
	switch {
	case crc.tinkerbellCluster.Spec.ControlPlaneEndpoint != nil && crc.tinkerbellCluster.Spec.ControlPlaneEndpoint.IsValid():
		// If the ControlPlaneEndpoint on tinkCluster is already configured, return it.
		return *crc.tinkerbellCluster.Spec.ControlPlaneEndpoint, nil
	case crc.cluster == nil:
		// If the owning cluster has not been set yet, error.
		return clusterv1.APIEndpoint{}, ErrClusterNotReady
	case crc.cluster.Spec.ControlPlaneEndpoint.IsValid():
		// If the ControlPlaneEndpoint on the cluster is already configured, return it.
		return crc.cluster.Spec.ControlPlaneEndpoint, nil
	}

	endpoint := clusterv1.APIEndpoint{
		Host: crc.cluster.Spec.ControlPlaneEndpoint.Host,
		Port: crc.cluster.Spec.ControlPlaneEndpoint.Port,
	}

	if tinkEP := crc.tinkerbellCluster.Spec.ControlPlaneEndpoint; tinkEP != nil {
		if endpoint.Host == "" {
			endpoint.Host = tinkEP.Host
		}

		if endpoint.Port == 0 {
			endpoint.Port = tinkEP.Port
		}
	}

	if endpoint.Host == "" {
		return endpoint, ErrControlPlaneEndpointNotSet
	}

	if endpoint.Port == 0 {
		endpoint.Port = KubernetesAPIPort
	}

	return endpoint, nil
}

// reconcileFailureDomains populates Status.FailureDomains from the distinct values of the
// Spec.FailureDomainLabel label across the cluster's Hardware.
//
// Every discovered domain is marked ControlPlane, because KubeadmControlPlane ignores any
// domain that is not (see ControlPlane.FailureDomains in the CAPI KCP internals). Domains
// are reported whether or not they currently hold unclaimed Hardware: a failure domain
// describes topology, not free capacity, so dropping an exhausted rack from the list would
// make the published set flap as machines come and go.
func (crc *clusterReconcileContext) reconcileFailureDomains() error {
	label := crc.tinkerbellCluster.Spec.FailureDomainLabel
	if label == "" {
		crc.tinkerbellCluster.Status.FailureDomains = nil

		return nil
	}

	hardwareList := &tinkv1.HardwareList{}
	if err := crc.tinkerbellClient.List(crc.ctx, hardwareList, client.HasLabels{label}); err != nil {
		return fmt.Errorf("listing Hardware for failure domains: %w", err)
	}

	seen := map[string]struct{}{}

	for i := range hardwareList.Items {
		// HasLabels matches on key presence, so an empty value reaches here. It cannot
		// name a failure domain (Machine.spec.failureDomain of "" means "unassigned"),
		// so skip it rather than publishing a domain nothing can be placed into.
		if value := hardwareList.Items[i].Labels[label]; value != "" {
			seen[value] = struct{}{}
		}
	}

	domains := make([]clusterv1.FailureDomain, 0, len(seen))
	for name := range seen {
		domains = append(domains, clusterv1.FailureDomain{
			Name:         name,
			ControlPlane: ptr.To(true),
		})
	}

	// Sorted for a deterministic order: this list is copied verbatim into
	// Cluster.status.failureDomains, and an unstable order would rewrite that status on
	// every pass and reconcile in a loop.
	sort.Slice(domains, func(i, j int) bool { return domains[i].Name < domains[j].Name })

	crc.tinkerbellCluster.Status.FailureDomains = domains

	return nil
}

// Reconcile implements ReconcileContext interface by ensuring that all TinkerbellCluster object
// fields are properly populated.
func (crc *clusterReconcileContext) reconcile() (ctrl.Result, error) {
	controlPlaneEndpoint, err := crc.controlPlaneEndpoint()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Ensure that we are setting the ControlPlaneEndpoint on the TinkerbellCluster
	// in the event that it was defined on the Cluster resource instead
	crc.tinkerbellCluster.Spec.ControlPlaneEndpoint = &clusterv1.APIEndpoint{
		Host: controlPlaneEndpoint.Host,
		Port: controlPlaneEndpoint.Port,
	}

	// Set both Ready and Initialization.Provisioned: CAPI checks status.ready
	// under the v1beta1 contract and status.initialization.provisioned under
	// v1beta2. Both are set to maintain backward compatibility during upgrades.
	crc.tinkerbellCluster.Status.Ready = true
	crc.tinkerbellCluster.Status.Initialization = &infrastructurev1.TinkerbellClusterInitializationStatus{
		Provisioned: ptr.To(true),
	}

	if err := crc.reconcileFailureDomains(); err != nil {
		return ctrl.Result{}, err
	}

	crc.log.Info("Setting cluster status to ready")

	if err := crc.patchHelper.Patch(crc.ctx, crc.tinkerbellCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching cluster object: %w", err)
	}

	// In local mode a Hardware watch re-triggers this reconcile, so no resync is needed.
	if crc.tinkerbellCluster.Spec.FailureDomainLabel == "" || !crc.externalTinkerbell {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: failureDomainResyncInterval}, nil
}

func (crc *clusterReconcileContext) reconcileDelete() error {
	return nil
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=tinkerbell.org,resources=hardware;hardware/status,verbs=get;list;watch

// Reconcile ensures state of Tinkerbell clusters.
func (tcr *TinkerbellClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	crc, err := tcr.newReconcileContext(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("creating reconciliation context: %w", err)
	}

	if crc == nil {
		return ctrl.Result{}, nil
	}

	isPaused, _, err := paused.EnsurePausedCondition(ctx, tcr.Client, crc.cluster, crc.tinkerbellCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensuring paused condition: %w", err)
	}

	if isPaused {
		crc.log.Info("TinkerbellCluster is paused, skipping reconciliation")

		return ctrl.Result{}, nil
	}

	if !crc.tinkerbellCluster.DeletionTimestamp.IsZero() {
		crc.log.Info("Removing cluster")

		return ctrl.Result{}, crc.reconcileDelete()
	}

	if crc.cluster == nil {
		return ctrl.Result{}, nil
	}

	return crc.reconcile()
}

// SetupWithManager configures reconciler with a given manager.
func (tcr *TinkerbellClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options, sm *runtime.Scheme) error {
	log := ctrl.LoggerFrom(ctx)

	mapper := util.ClusterToInfrastructureMapFunc(
		ctx,
		infrastructurev1.GroupVersion.WithKind("TinkerbellCluster"),
		mgr.GetClient(),
		&infrastructurev1.TinkerbellCluster{},
	)

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&infrastructurev1.TinkerbellCluster{}).
		WithEventFilter(predicates.ResourceHasFilterLabel(sm, log, tcr.WatchFilterValue)).
		WithEventFilter(predicates.ResourceIsNotExternallyManaged(sm, log)).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(mapper),
			builder.WithPredicates(predicates.ClusterPausedTransitionsOrInfrastructureProvisioned(sm, log)),
		)

	// Keep status.failureDomains current as Hardware is racked, retired or relabelled.
	// Only in local mode: external Hardware lives in a cluster this manager's cache does
	// not watch, and is picked up by the resync in reconcile() instead.
	if !tcr.ExternalTinkerbell {
		controllerBuilder = controllerBuilder.Watches(
			&tinkv1.Hardware{},
			handler.EnqueueRequestsFromMapFunc(tcr.hardwareToFailureDomainClusters),
		)
	}

	if err := controllerBuilder.Complete(tcr); err != nil {
		return fmt.Errorf("failed to configure controller: %w", err)
	}

	return nil
}

// hardwareToFailureDomainClusters enqueues every TinkerbellCluster that discovers failure
// domains, whatever the Hardware that changed.
//
// Hardware carries no reference to a cluster, and the label the clusters key on is not
// known here, so there is nothing to narrow on. Filtering by the changed Hardware's own
// labels would also miss the case that matters most: a Hardware losing its topology label
// arrives with the label already gone, and would stop mapping to the very cluster whose
// published domains have just gone stale. The list of clusters is small and the reconcile
// it triggers is a no-op patch when nothing moved.
func (tcr *TinkerbellClusterReconciler) hardwareToFailureDomainClusters(ctx context.Context, _ client.Object) []ctrl.Request {
	log := ctrl.LoggerFrom(ctx)

	clusterList := &infrastructurev1.TinkerbellClusterList{}
	if err := tcr.List(ctx, clusterList); err != nil {
		log.Error(err, "Failed to list TinkerbellClusters for a Hardware event")

		return nil
	}

	var requests []ctrl.Request

	for i := range clusterList.Items {
		if clusterList.Items[i].Spec.FailureDomainLabel == "" {
			continue
		}

		requests = append(requests, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      clusterList.Items[i].Name,
				Namespace: clusterList.Items[i].Namespace,
			},
		})
	}

	return requests
}
