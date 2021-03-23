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

// Package controllers contains Cluster API controllers for Tinkerbell.
package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrastructurev1alpha3 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

// TinkerbellClusterReconciler implements Reconciler interface.
type TinkerbellClusterReconciler struct {
	Log    logr.Logger
	Client client.Client
}

// validate validates if context configuration has all required fields properly populated.
func (tcr *TinkerbellClusterReconciler) validate() error {
	if tcr.Log == nil {
		return fmt.Errorf("logger is nil")
	}

	if tcr.Client == nil {
		return fmt.Errorf("client is nil")
	}

	return nil
}

// New builds a context for cluster reconciliation process, collecting all required
// information.
//
// If unexpected case occurs, error is returned.
//
// If some data is not yet available, nil is returned.
//
//nolint:lll
func (tcr *TinkerbellClusterReconciler) newReconcileContext(ctx context.Context, namespacedName types.NamespacedName) (*clusterReconcileContext, error) {
	if err := tcr.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	crc := &clusterReconcileContext{
		log:               tcr.Log.WithValues("tinkerbellcluster", namespacedName),
		ctx:               ctx,
		tinkerbellCluster: &infrastructurev1alpha3.TinkerbellCluster{},
		client:            tcr.Client,
		namespacedName:    namespacedName,
	}

	if err := crc.client.Get(crc.ctx, namespacedName, crc.tinkerbellCluster); err != nil {
		if apierrors.IsNotFound(err) {
			crc.log.Info("TinkerbellCluster object not found")

			return nil, nil
		}

		return nil, fmt.Errorf("getting TinkerbellCluster: %w", err)
	}

	crc.log = crc.log.WithName(crc.tinkerbellCluster.APIVersion)

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
		crc.log.Info("OwnerCluster is not set yet. Requeuing...")
	}

	crc.cluster = cluster

	return crc, nil
}

// clusterReconcileContext implements ReconcileContext by reconciling TinkerbellCluster object.
type clusterReconcileContext struct {
	ctx               context.Context
	tinkerbellCluster *infrastructurev1alpha3.TinkerbellCluster
	patchHelper       *patch.Helper
	cluster           *clusterv1.Cluster
	log               logr.Logger
	client            client.Client
	namespacedName    types.NamespacedName
}

const (
	// HardwareOwnerNameLabel is a label set by either CAPT controllers or Tinkerbell controller to indicate
	// that given hardware takes part of at least one workflow.
	HardwareOwnerNameLabel = "v1alpha1.tinkerbell.org/ownerName"

	// HardwareOwnerNamespaceLabel is a label set by either CAPT controllers or Tinkerbell controller to indicate
	// that given hardware takes part of at least one workflow.
	HardwareOwnerNamespaceLabel = "v1alpha1.tinkerbell.org/ownerNamespace"

	// ClusterNameLabel is used to mark Hardware as assigned controlplane machine.
	ClusterNameLabel = "v1alpha1.tinkerbell.org/clusterName"

	// ClusterNamespaceLabel is used to mark in which Namespace hardware is used.
	ClusterNamespaceLabel = "v1alpha1.tinkerbell.org/clusterNamespace"

	// KubernetesAPIPort is a port used by Tinkerbell clusters for Kubernetes API.
	KubernetesAPIPort = 6443
)

func nextAvailableHardware(ctx context.Context, k8sClient client.Client, extraSelectors []string) (*tinkv1alpha1.Hardware, error) { //nolint:lll
	hardware, err := nextHardware(ctx, k8sClient, append(extraSelectors, fmt.Sprintf("!%s", HardwareOwnerNameLabel)))
	if err != nil {
		return nil, fmt.Errorf("getting next Hardware object: %w", err)
	}

	if hardware == nil {
		return nil, fmt.Errorf("no hardware available")
	}

	return hardware, nil
}

func nextHardware(ctx context.Context, k8sClient client.Client, selectors []string) (*tinkv1alpha1.Hardware, error) { //nolint:lll
	availableHardwares := &tinkv1alpha1.HardwareList{}

	selectorsRaw := strings.Join(selectors, ",")

	selector, err := labels.Parse(selectorsRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing raw labels selector %q: %w", selectorsRaw, err)
	}

	options := client.MatchingLabelsSelector{
		Selector: selector,
	}

	if err := k8sClient.List(ctx, availableHardwares, options); err != nil {
		return nil, fmt.Errorf("listing hardware without owner: %w", err)
	}

	if len(availableHardwares.Items) == 0 {
		return nil, nil
	}

	return &availableHardwares.Items[0], nil
}

func hardwareIP(hardware *tinkv1alpha1.Hardware) (string, error) {
	if hardware == nil {
		return "", fmt.Errorf("given Hardware object is nil")
	}

	if len(hardware.Status.Interfaces) == 0 {
		return "", fmt.Errorf("hardware has no interfaces defined")
	}

	if hardware.Status.Interfaces[0].DHCP == nil {
		return "", fmt.Errorf("hardware's first interface has no DHCP address defined")
	}

	if hardware.Status.Interfaces[0].DHCP.IP == nil {
		return "", fmt.Errorf("hardware's first interface has no DHCP IP address defined")
	}

	if hardware.Status.Interfaces[0].DHCP.IP.Address == "" {
		return "", fmt.Errorf("hardware's first interface has no DHCP IP address is empty")
	}

	return hardware.Status.Interfaces[0].DHCP.IP.Address, nil
}

func (crc *clusterReconcileContext) takeHardwareOwnership(hardware *tinkv1alpha1.Hardware) error {
	patchHelper, err := patch.NewHelper(hardware, crc.client)
	if err != nil {
		return fmt.Errorf("creating patch helper: %w", err)
	}

	if len(hardware.ObjectMeta.Labels) == 0 {
		hardware.ObjectMeta.Labels = map[string]string{}
	}

	hardware.ObjectMeta.Labels[ClusterNameLabel] = crc.tinkerbellCluster.Name
	hardware.ObjectMeta.Labels[ClusterNamespaceLabel] = crc.tinkerbellCluster.Namespace

	controllerutil.AddFinalizer(hardware, infrastructurev1alpha3.ClusterFinalizer)

	if err := patchHelper.Patch(crc.ctx, hardware); err != nil {
		return fmt.Errorf("patching Hardware object with cluster label: %w", err)
	}

	return nil
}

func (crc *clusterReconcileContext) populateControlplaneHost() error {
	hardware, err := nextAvailableHardware(crc.ctx, crc.client, []string{fmt.Sprintf("!%s", ClusterNameLabel)})
	if err != nil {
		return fmt.Errorf("getting next available hardware: %w", err)
	}

	ip, err := hardwareIP(hardware)
	if err != nil {
		return fmt.Errorf("getting Hardware IP address: %w", err)
	}

	crc.log.Info("Assigning IP to cluster", "ip", ip, "clusterName", crc.tinkerbellCluster.Name)

	crc.tinkerbellCluster.Spec.ControlPlaneEndpoint.Host = ip

	if err := crc.takeHardwareOwnership(hardware); err != nil {
		return fmt.Errorf("taking Hardware ownership: %w", err)
	}

	return nil
}

// Reconcile implements ReconcileContext interface by ensuring that all TinkerbellCluster object
// fields are properly populated.
func (crc *clusterReconcileContext) reconcile() error {
	if crc.tinkerbellCluster.Spec.ControlPlaneEndpoint.Host == "" {
		if err := crc.populateControlplaneHost(); err != nil {
			return fmt.Errorf("populating controlplane host: %w", err)
		}
	}

	// TODO: How can we support changing that?
	if crc.tinkerbellCluster.Spec.ControlPlaneEndpoint.Port != KubernetesAPIPort {
		crc.tinkerbellCluster.Spec.ControlPlaneEndpoint.Port = KubernetesAPIPort
	}

	crc.tinkerbellCluster.Status.Ready = true

	controllerutil.AddFinalizer(crc.tinkerbellCluster, infrastructurev1alpha3.ClusterFinalizer)

	crc.log.Info("Setting cluster status to ready")

	if err := crc.patchHelper.Patch(crc.ctx, crc.tinkerbellCluster); err != nil {
		return fmt.Errorf("patching cluster object: %w", err)
	}

	return nil
}

func controlplaneNodeSelector(tinkerbellCluster *infrastructurev1alpha3.TinkerbellCluster) string {
	return fmt.Sprintf("%s=%s,%s=%s",
		ClusterNameLabel, tinkerbellCluster.Name,
		ClusterNamespaceLabel, tinkerbellCluster.Namespace)
}

func (crc *clusterReconcileContext) releaseHardware() error {
	selector := []string{controlplaneNodeSelector(crc.tinkerbellCluster)}

	hardware, err := nextHardware(crc.ctx, crc.client, selector)
	if err != nil {
		return fmt.Errorf("getting controlplane Hardware: %w", err)
	}

	if hardware == nil {
		crc.log.Info("Hardware has already been released")

		return nil
	}

	patchHelper, err := patch.NewHelper(hardware, crc.client)
	if err != nil {
		return fmt.Errorf("creating patch helper to release Hardware: %w", err)
	}

	delete(hardware.ObjectMeta.Labels, ClusterNameLabel)
	delete(hardware.ObjectMeta.Labels, ClusterNamespaceLabel)

	controllerutil.RemoveFinalizer(hardware, infrastructurev1alpha3.ClusterFinalizer)

	if err := patchHelper.Patch(crc.ctx, hardware); err != nil {
		return fmt.Errorf("patching Hardware object to remove cluster label: %w", err)
	}

	return nil
}

func (crc *clusterReconcileContext) reconcileDelete() error {
	crc.log.Info("Releasing owned Hardware")

	if err := crc.releaseHardware(); err != nil {
		return fmt.Errorf("releasing Hardware: %w", err)
	}

	controllerutil.RemoveFinalizer(crc.tinkerbellCluster, infrastructurev1alpha3.ClusterFinalizer)

	if err := crc.patchHelper.Patch(crc.ctx, crc.tinkerbellCluster); err != nil {
		return fmt.Errorf("patching cluster object with removed finalizer: %w", err)
	}

	return nil
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tinkerbellclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

// Reconcile ensures state of Tinkerbell clusters.
func (tcr *TinkerbellClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	crc, err := tcr.newReconcileContext(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("creating reconciliation context: %w", err)
	}

	if crc == nil {
		return defaultRequeueResult(), nil
	}

	if !crc.tinkerbellCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		crc.log.Info("Removing cluster")

		return ctrl.Result{}, crc.reconcileDelete()
	}

	if crc.cluster == nil {
		return defaultRequeueResult(), nil
	}

	if util.IsPaused(crc.cluster, crc.tinkerbellCluster) {
		crc.log.Info("TinkerbellCluster is marked as paused. Won't reconcile")

		return defaultRequeueResult(), nil
	}

	return ctrl.Result{}, crc.reconcile()
}

// SetupWithManager configures reconciler with a given manager.
func (tcr *TinkerbellClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha3.TinkerbellCluster{}).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.ClusterToInfrastructureMapFunc(infrastructurev1alpha3.GroupVersion.WithKind("TinkerbellCluster")),
			},
		).
		Complete(tcr)
}
