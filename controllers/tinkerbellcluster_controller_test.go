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

package controllers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha3 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controllers"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

//nolint:unparam
func unreadyTinkerbellCluster(name, namespace string) *infrastructurev1alpha3.TinkerbellCluster {
	unreadyTinkerbellCluster := validTinkerbellCluster(name, clusterNamespace)
	unreadyTinkerbellCluster.Status.Ready = false
	unreadyTinkerbellCluster.ObjectMeta.Finalizers = nil
	unreadyTinkerbellCluster.Spec.ControlPlaneEndpoint.Host = ""
	unreadyTinkerbellCluster.Spec.ControlPlaneEndpoint.Port = 0

	return unreadyTinkerbellCluster
}

//nolint:funlen,gocognit
func Test_Cluster_reconciliation_with_available_hardware(t *testing.T) {
	t.Parallel()

	objects := []runtime.Object{
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileClusterWithClient(client, clusterName, clusterNamespace); err != nil {
		t.Fatalf("Reconciling with available hardware should succeed, got: %v", err)
	}

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1alpha3.TinkerbellCluster{}

	if err := client.Get(context.Background(), namespacedName, updatedTinkerbellCluster); err != nil {
		t.Fatalf("Getting updated Tinkerbell Cluster object: %v", err)
	}

	// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior.
	t.Run("sets_controlplane_endpoint_host_with_IP_address_of_selected_hardware", func(t *testing.T) {
		t.Parallel()

		endpoint := updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host
		if endpoint != hardwareIP {
			t.Fatalf("Expected controlplane endpoint to be set to %q, got: %q", hardwareIP, endpoint)
		}
	})

	// TODO: Verify if we actually need to set port, maybe we can use "default" one?
	t.Run("sets_controlplane_endpoint_port_with_hardcoded_value", func(t *testing.T) {
		t.Parallel()

		if updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Port != controllers.KubernetesAPIPort {
			t.Fatalf("Expected port %d, got %d", controllers.KubernetesAPIPort,
				updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Port)
		}
	})

	// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior.
	t.Run("sets_infrastructure_status_to_ready", func(t *testing.T) {
		t.Parallel()

		if !updatedTinkerbellCluster.Status.Ready {
			t.Fatalf("Expected infrastructure to be ready when hardware is assigned")
		}
	})

	// To ensure reconcile runs when cluster is removed to release used hardware.
	t.Run("sets_tinkerbell_finalizer_on_cluster_object", func(t *testing.T) {
		t.Parallel()

		if len(updatedTinkerbellCluster.ObjectMeta.Finalizers) == 0 {
			t.Fatalf("Expected at least one finalizer to be set")
		}

		if updatedTinkerbellCluster.ObjectMeta.Finalizers[0] != infrastructurev1alpha3.ClusterFinalizer {
			t.Fatalf("Expected first finalizer to be Tinkerbell cluster finalizer")
		}
	})

	t.Run("updates_hardware_selected_for_controlplane_with", func(t *testing.T) {
		t.Parallel()

		updatedHardware := &tinkv1alpha1.Hardware{}

		namespacedName := types.NamespacedName{
			Name: hardwareName,
		}

		if err := client.Get(context.Background(), namespacedName, updatedHardware); err != nil {
			t.Fatalf("Getting updated Hardware object: %v", err)
		}

		// Cluster controller does not provision machines itself, but must reserve the IP address of the machine
		// for controlplane endpoint. Reservation is done using "cluster" label together with "owner" label provided
		// by SHIM on Hardware objects.
		//
		// TODO: Make SHIM define and set "owner" label for Hardwares which participated in at least one workflow to
		// something like "external".
		t.Run("cluster_labels", func(t *testing.T) {
			t.Parallel()

			clusterNameLabel, ok := updatedHardware.ObjectMeta.Labels[controllers.ClusterNameLabel]
			if !ok {
				t.Fatalf("Cluster label name missing on Hardware object")
			}

			if clusterNameLabel != clusterName {
				t.Fatalf("Expected label value %q, got %q", clusterName, clusterNameLabel)
			}

			namespaceNameLabel, ok := updatedHardware.ObjectMeta.Labels[controllers.ClusterNamespaceLabel]
			if !ok {
				t.Fatalf("Cluster label namespace missing on Hardware object")
			}

			if namespaceNameLabel != clusterNamespace {
				t.Fatalf("Expected label value %q, got %q", clusterNamespace, namespaceNameLabel)
			}
		})

		t.Run("tinkerbell_cluster_finalizer", func(t *testing.T) {
			t.Parallel()

			if len(updatedHardware.ObjectMeta.Finalizers) == 0 {
				t.Fatalf("Expected at least one finalizer to be set")
			}

			if updatedHardware.ObjectMeta.Finalizers[0] != infrastructurev1alpha3.ClusterFinalizer {
				t.Fatalf("Expected first finalizer to be Tinkerbell cluster finalizer")
			}
		})
	})
}

func Test_Cluster_reconciliation(t *testing.T) {
	t.Parallel()

	t.Run("is_requeued_when", func(t *testing.T) {
		t.Parallel()

		// This is introduced in v1alpha3 of CAPI even though behavior diagram does not reflect it.
		t.Run("cluster_is_paused", clusterReconciliationIsRequeuedWhenClusterIsPaused)

		// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior.
		t.Run("cluster_has_no_owner_set", clusterReconciliationIsRequeuedWhenClusterHasNoOwnerSet)
	})

	// If reconciliation process started, but we cannot find cluster object anymore, it means object has been
	// removed in the meanwhile. This means there is nothing to do.
	t.Run("is_not_requeued_when_cluster_object_is_missing", //nolint:paralleltest
		clusterReconciliationIsNotRequeuedWhenClusterObjectIsMissing)

	t.Run("fails_when", func(t *testing.T) {
		t.Parallel()

		t.Run("reconciler_has_no_logger_set", clusterReconciliationFailsWhenReconcilerHasNoLoggerSet)
		t.Run("reconciler_has_no_client_set", clusterReconciliationFailsWhenReconcilerHasNoClientSet)

		// At least one available hardware is required to be reserved for controlplane endpoint.
		t.Run("there_is_no_hardware_available", clusterReconciliationFailsWhenThereIsNoHardwareAvailable)

		// If all hardwares has owner label set, then we cannot add new clusters.
		t.Run("all_available_hardware_is_occupied", clusterReconciliationFailsWhenAllAvailableHardwareIsOccupied)

		// Validate against malformed hardware.
		t.Run("selected_hardware_has_no_interfaces_configured",
			clusterReconciliationFailsWhenSelectedhardwareHasNoInterfacesConfigured)
		t.Run("selected_hardware_has_no_DHCP_configured_on_first_interface",
			clusterReconciliationFailsWhenSelectedhardwareHasNoDHCPConfiguredOnFirstInterface)
		t.Run("selected_hardware_has_no_DHCP_IP_configured_on_first_interface",
			clusterReconciliationFailsWhenSelectedhardwareHasNoDHCPIPConfiguredOnFirstInterface)
		t.Run("selected_hardware_has_empty_DHCP_IP_configured_on_first_interface",
			clusterReconciliationFailsWhenSelectedhardwareHasEmptyDHCPIPConfiguredOnFirstInterface)
	})

	// Some sort of locking mechanism must be used so each reconciled cluster gets unique IP address.
	t.Run("assigns_unique_controlplane_endpoint_for_each_cluster", //nolint:paralleltest
		clusterReconciliationAssignsUniqueControlplaneEndpointForEachCluster)
}

//nolint:funlen
func Test_Cluster_reconciliation_when_cluster_is_scheduled_for_removal_it(t *testing.T) {
	t.Parallel()

	now := metav1.Now()

	tinkerbellClusterScheduledForRemoval := validTinkerbellCluster(clusterName, clusterNamespace)
	tinkerbellClusterScheduledForRemoval.DeletionTimestamp = &now

	occupiedHardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	occupiedHardware.ObjectMeta.Labels = map[string]string{
		controllers.ClusterNameLabel:      clusterName,
		controllers.ClusterNamespaceLabel: clusterNamespace,
	}
	occupiedHardware.ObjectMeta.Finalizers = []string{infrastructurev1alpha3.ClusterFinalizer}

	objects := []runtime.Object{
		occupiedHardware,
		validCluster(clusterName, clusterNamespace),
		tinkerbellClusterScheduledForRemoval,
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileClusterWithClient(client, clusterName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected error while reconciling: %v", err)
	}

	t.Run("marks_hardware_as_available_for_other_clusters_by", func(t *testing.T) {
		t.Parallel()

		updatedHardware := &tinkv1alpha1.Hardware{}

		namespacedName := types.NamespacedName{
			Name: hardwareName,
		}

		if err := client.Get(context.Background(), namespacedName, updatedHardware); err != nil {
			t.Fatalf("Getting updated Hardware object: %v", err)
		}

		t.Run("removing_cluster_labels", func(t *testing.T) {
			t.Parallel()

			if _, ok := updatedHardware.ObjectMeta.Labels[controllers.ClusterNameLabel]; ok {
				t.Fatalf("Cluster name label has not been removed from Hardware object")
			}

			if _, ok := updatedHardware.ObjectMeta.Labels[controllers.ClusterNamespaceLabel]; ok {
				t.Fatalf("Cluster namespace label has not been removed from Hardware object")
			}
		})

		t.Run("removing_tinkerbell_cluster_finalizer", func(t *testing.T) {
			t.Parallel()

			if len(updatedHardware.ObjectMeta.Finalizers) != 0 {
				t.Fatalf("Expected all finalizers to be removed")
			}
		})
	})

	// This makes sure that Cluster object can be gracefully removed.
	//
	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#behavior
	t.Run("removes_tinkerbell_finalizer_from_cluster_object", func(t *testing.T) {
		t.Parallel()

		updatedTinkerbellCluster := &infrastructurev1alpha3.TinkerbellCluster{}

		namespacedName := types.NamespacedName{
			Name:      clusterName,
			Namespace: clusterNamespace,
		}

		if err := client.Get(context.Background(), namespacedName, updatedTinkerbellCluster); err != nil {
			t.Fatalf("Getting updated Tinkerbell Cluster object: %v", err)
		}

		if finalizers := updatedTinkerbellCluster.ObjectMeta.Finalizers; len(finalizers) != 0 {
			t.Fatalf("Unexpected finalizers: %v", finalizers)
		}
	})
}

// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#behavior
func Test_Cluster_reconciliation_when_cluster_is_scheduled_for_removal_it_removes_finalizer_for_cluster_without_owner(t *testing.T) { //nolint:lll
	t.Parallel()

	now := metav1.Now()

	clusterScheduledForRemovalWithoutOwner := validTinkerbellCluster(clusterName, clusterNamespace)
	clusterScheduledForRemovalWithoutOwner.ObjectMeta.DeletionTimestamp = &now
	clusterScheduledForRemovalWithoutOwner.ObjectMeta.OwnerReferences = nil

	occupiedHardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	occupiedHardware.ObjectMeta.Labels = map[string]string{
		controllers.ClusterNameLabel:      clusterName,
		controllers.ClusterNamespaceLabel: clusterNamespace,
	}
	occupiedHardware.ObjectMeta.Finalizers = []string{infrastructurev1alpha3.ClusterFinalizer}

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		clusterScheduledForRemovalWithoutOwner,
		occupiedHardware,
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileClusterWithClient(client, clusterName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected error while reconciling: %v", err)
	}

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1alpha3.TinkerbellCluster{}

	if err := client.Get(context.Background(), namespacedName, updatedTinkerbellCluster); err != nil {
		t.Fatalf("Getting updated Tinkerbell Cluster object: %v", err)
	}

	if finalizers := updatedTinkerbellCluster.ObjectMeta.Finalizers; len(finalizers) != 0 {
		t.Fatalf("Unexpected finalizers: %v", finalizers)
	}
}

// If removal process is interrupted at the moment we released the hardware, but we didn't get a chance to patch the
// cluster object to remove the finalizer. This prevents removal from getting stuck while interrupted.
func Test_Cluster_reconciliation_when_cluster_is_scheduled_for_removal_it_removes_finalizer_for_cluster_without_hardware(t *testing.T) { //nolint:lll
	t.Parallel()

	now := metav1.Now()

	clusterScheduledForRemoval := validTinkerbellCluster(clusterName, clusterNamespace)
	clusterScheduledForRemoval.ObjectMeta.DeletionTimestamp = &now

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		clusterScheduledForRemoval,
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileClusterWithClient(client, clusterName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected error while reconciling: %v", err)
	}

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1alpha3.TinkerbellCluster{}

	if err := client.Get(context.Background(), namespacedName, updatedTinkerbellCluster); err != nil {
		t.Fatalf("Getting updated Tinkerbell Cluster object: %v", err)
	}

	if finalizers := updatedTinkerbellCluster.ObjectMeta.Finalizers; len(finalizers) != 0 {
		t.Fatalf("Unexpected finalizers: %v", finalizers)
	}
}

func clusterReconciliationFailsWhenReconcilerHasNoLoggerSet(t *testing.T) {
	t.Parallel()

	clusterController := &controllers.TinkerbellClusterReconciler{
		Log: logr.Discard(),
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}

	if _, err := clusterController.Reconcile(request); err == nil {
		t.Fatalf("Expected error while reconcilign")
	}
}

func clusterReconciliationFailsWhenReconcilerHasNoClientSet(t *testing.T) {
	t.Parallel()

	clusterController := &controllers.TinkerbellClusterReconciler{
		Client: fake.NewFakeClient(),
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}

	if _, err := clusterController.Reconcile(request); err == nil {
		t.Fatalf("Expected error while reconciling")
	}
}

func clusterReconciliationFailsWhenSelectedhardwareHasNoInterfacesConfigured(t *testing.T) {
	t.Parallel()

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces = []tinkv1alpha1.Interface{}

	if _, err := reconcileWithHardwares(t, []*tinkv1alpha1.Hardware{hardware}); err == nil {
		t.Fatalf("Expected reconciliation error")
	}
}

func clusterReconciliationFailsWhenSelectedhardwareHasNoDHCPConfiguredOnFirstInterface(t *testing.T) {
	t.Parallel()

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces[0].DHCP = nil

	if _, err := reconcileWithHardwares(t, []*tinkv1alpha1.Hardware{hardware}); err == nil {
		t.Fatalf("Expected reconciliation error")
	}
}

func clusterReconciliationFailsWhenSelectedhardwareHasNoDHCPIPConfiguredOnFirstInterface(t *testing.T) {
	t.Parallel()

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces[0].DHCP.IP = nil

	if _, err := reconcileWithHardwares(t, []*tinkv1alpha1.Hardware{hardware}); err == nil {
		t.Fatalf("Expected reconciliation error")
	}
}

func clusterReconciliationFailsWhenSelectedhardwareHasEmptyDHCPIPConfiguredOnFirstInterface(t *testing.T) {
	t.Parallel()

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces[0].DHCP.IP.Address = ""

	if _, err := reconcileWithHardwares(t, []*tinkv1alpha1.Hardware{hardware}); err == nil {
		t.Fatalf("Expected reconciliation error")
	}
}

func kubernetesClientWithObjects(t *testing.T, objects []runtime.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()

	if err := tinkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding Tinkerbell objects to scheme should succeed, got: %v", err)
	}

	if err := infrastructurev1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding Tinkerbell CAPI objects to scheme should succeed, got: %v", err)
	}

	if err := clusterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CAPI objects to scheme should succeed, got: %v", err)
	}

	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding Core V1 objects to scheme should succeed, got: %v", err)
	}

	fakeClient := fake.NewFakeClientWithScheme(scheme, objects...)

	return fakeClient
}

//nolint:unparam
func reconcileClusterWithClient(client client.Client, name, namespace string) (ctrl.Result, error) {
	log := logr.Discard()

	clusterController := &controllers.TinkerbellClusterReconciler{
		Log:    log,
		Client: client,
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	return clusterController.Reconcile(request)
}

func clusterReconciliationIsNotRequeuedWhenClusterObjectIsMissing(t *testing.T) {
	t.Parallel()

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, nil), clusterName, clusterNamespace)
	if err != nil {
		t.Fatalf("Reconciling when cluster object does not exist should not return error")
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("Expected non-zero requeue time")
	}
}

const (
	clusterName      = "myClusterName"
	clusterNamespace = "myClusterNamespace"
	hardwareIP       = "1.1.1.1"
	hardwareName     = "myHardwareName"
)

func clusterReconciliationFailsWhenThereIsNoHardwareAvailable(t *testing.T) {
	t.Parallel()

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	_, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling new cluster object should fail when there is no hardware available")
	}

	if !strings.Contains(err.Error(), "no hardware available") {
		t.Fatalf("Error should indicate that hardware is not available, got: %v", err)
	}
}

func clusterReconciliationIsRequeuedWhenClusterHasNoOwnerSet(t *testing.T) {
	t.Parallel()

	unreadyTinkerbellClusterWithoutOwner := unreadyTinkerbellCluster(clusterName, clusterNamespace)
	unreadyTinkerbellClusterWithoutOwner.ObjectMeta.OwnerReferences = nil

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellClusterWithoutOwner,
	}

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	if err != nil {
		t.Fatalf("Reconciling new cluster object should not fail when cluster has no owner set yet")
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("RequeueAfter is zero")
	}
}

func clusterReconciliationIsRequeuedWhenClusterIsPaused(t *testing.T) {
	t.Parallel()

	pausedTinkerbellCluster := validTinkerbellCluster(clusterName, clusterNamespace)
	pausedTinkerbellCluster.ObjectMeta.Annotations = map[string]string{
		clusterv1.PausedAnnotation: "true",
	}

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		pausedTinkerbellCluster,
	}

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	if err != nil {
		t.Fatalf("Reconciling new cluster object should not fail when cluster has no owner set yet, got: %v", err)
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("RequeueAfter is zero")
	}
}

func reconcileWithHardwares(t *testing.T, hardwares []*tinkv1alpha1.Hardware) (ctrl.Result, error) {
	t.Helper()

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	for _, hw := range hardwares {
		objects = append(objects, hw)
	}

	return reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
}

func clusterReconciliationFailsWhenAllAvailableHardwareIsOccupied(t *testing.T) {
	t.Parallel()

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)

	hardware.ObjectMeta.Labels = map[string]string{
		controllers.HardwareOwnerNameLabel:      "foo",
		controllers.HardwareOwnerNamespaceLabel: "bar",
	}

	if _, err := reconcileWithHardwares(t, []*tinkv1alpha1.Hardware{hardware}); err == nil {
		t.Fatalf("Expected reconciliation error")
	}
}

// When multiple clusters are created simultaneously, each should get different IP address.
func clusterReconciliationAssignsUniqueControlplaneEndpointForEachCluster(t *testing.T) {
	t.Parallel()

	firstClusterName := "firstClusterName"
	secondClusterName := "secondClusterName"

	firstHardwareUUID := uuid.New().String()
	firstHardwareIP := hardwareIP
	firstHardwareName := hardwareName

	secondHardwareUUID := uuid.New().String()
	secondHardwareIP := "2.2.2.2"
	secondHardwareName := "secondHardwareName"

	objects := []runtime.Object{
		validHardware(firstHardwareName, firstHardwareUUID, firstHardwareIP),
		validHardware(secondHardwareName, secondHardwareUUID, secondHardwareIP),

		validCluster(firstClusterName, clusterNamespace),
		validCluster(secondClusterName, clusterNamespace),

		unreadyTinkerbellCluster(firstClusterName, clusterNamespace),
		unreadyTinkerbellCluster(secondClusterName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileClusterWithClient(client, firstClusterName, clusterNamespace); err != nil {
		t.Fatalf("Reconciling with available hardware should succeed, got: %v", err)
	}

	if _, err := reconcileClusterWithClient(client, secondClusterName, clusterNamespace); err != nil {
		t.Fatalf("Reconciling with available hardware should succeed, got: %v", err)
	}

	clusterNamespacedName := types.NamespacedName{
		Name:      firstClusterName,
		Namespace: clusterNamespace,
	}

	firstUpdatedTinkerbellCluster := &infrastructurev1alpha3.TinkerbellCluster{}

	if err := client.Get(context.Background(), clusterNamespacedName, firstUpdatedTinkerbellCluster); err != nil {
		t.Fatalf("Getting second updated Tinkerbell Cluster object: %v", err)
	}

	secondUpdatedTinkerbellCluster := &infrastructurev1alpha3.TinkerbellCluster{}

	clusterNamespacedName.Name = secondClusterName

	if err := client.Get(context.Background(), clusterNamespacedName, secondUpdatedTinkerbellCluster); err != nil {
		t.Fatalf("Getting second updated Tinkerbell Cluster object: %v", err)
	}

	firstHost := firstUpdatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host
	secondHost := secondUpdatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host

	if firstHost == secondHost {
		t.Fatalf("Each cluster should get unique IP address, got %q twice", firstHost)
	}
}
