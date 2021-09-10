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
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha4"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controllers"
	tinkv1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

//nolint:unparam
func unreadyTinkerbellCluster(name, namespace string) *infrastructurev1.TinkerbellCluster {
	unreadyTinkerbellCluster := validTinkerbellCluster(name, clusterNamespace)
	unreadyTinkerbellCluster.Status.Ready = false
	unreadyTinkerbellCluster.ObjectMeta.Finalizers = nil
	unreadyTinkerbellCluster.Spec.ControlPlaneEndpoint.Host = ""
	unreadyTinkerbellCluster.Spec.ControlPlaneEndpoint.Port = 0

	return unreadyTinkerbellCluster
}

//nolint:funlen
func Test_Cluster_reconciliation_with_available_hardware(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	objects := []runtime.Object{
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling with available hardware should succeed")

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	g.Expect(client.Get(context.Background(), namespacedName, updatedTinkerbellCluster)).To(Succeed())

	// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior.
	t.Run("sets_controlplane_endpoint_host_with_IP_address_of_selected_hardware", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		endpoint := updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host
		g.Expect(endpoint).To(BeEquivalentTo(hardwareIP),
			"Expected controlplane endpoint to be set to %q, got: %q", hardwareIP, endpoint)
	})

	// TODO: Verify if we actually need to set port, maybe we can use "default" one?
	t.Run("sets_controlplane_endpoint_port_with_hardcoded_value", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		g.Expect(updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Port).To(BeEquivalentTo(controllers.KubernetesAPIPort),
			"Expected port %d, got %d", controllers.KubernetesAPIPort, updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Port)
	})

	// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior.
	t.Run("sets_infrastructure_status_to_ready", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		g.Expect(updatedTinkerbellCluster.Status.Ready).To(BeTrue(),
			"Expected infrastructure to be ready when hardware is assigned")
	})

	// To ensure reconcile runs when cluster is removed to release used hardware.
	t.Run("sets_tinkerbell_finalizer_on_cluster_object", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		g.Expect(updatedTinkerbellCluster.ObjectMeta.Finalizers).NotTo(BeEmpty(),
			"Expected at least one finalizer to be set")

		g.Expect(updatedTinkerbellCluster.ObjectMeta.Finalizers).To(ContainElement(infrastructurev1.ClusterFinalizer),
			"Expected finalizers to contain Tinkerbell cluster finalizer")
	})

	t.Run("updates_hardware_selected_for_controlplane_with", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		updatedHardware := &tinkv1.Hardware{}

		namespacedName := types.NamespacedName{
			Name: hardwareName,
		}

		g.Expect(client.Get(context.Background(), namespacedName, updatedHardware)).To(Succeed())

		// Cluster controller does not provision machines itself, but must reserve the IP address of the machine
		// for controlplane endpoint. Reservation is done using "cluster" label together with "owner" label provided
		// by SHIM on Hardware objects.
		//
		// TODO: Make SHIM define and set "owner" label for Hardwares which participated in at least one workflow to
		// something like "external".
		t.Run("cluster_labels", func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			g.Expect(updatedHardware.ObjectMeta.Labels).To(HaveKeyWithValue(controllers.ClusterNameLabel, clusterName),
				"Expected Hardware object to have the cluster name label set")

			g.Expect(updatedHardware.ObjectMeta.Labels).To(HaveKeyWithValue(controllers.ClusterNamespaceLabel, clusterNamespace),
				"Expected Hardware object to have the cluster namespace label set")
		})

		t.Run("tinkerbell_cluster_finalizer", func(t *testing.T) {
			t.Parallel()

			g.Expect(updatedHardware.ObjectMeta.Finalizers).NotTo(BeEmpty(),
				"Expected at least one finalizer to be set")

			g.Expect(updatedHardware.ObjectMeta.Finalizers).To(ContainElement(infrastructurev1.ClusterFinalizer),
				"Expected finalizers to contain Tinkerbell cluster finalizer")
		})
	})
}

func Test_Cluster_reconciliation(t *testing.T) {
	t.Parallel()

	t.Run("is_not_requeued_when", func(t *testing.T) {
		t.Parallel()

		// This is introduced in v1alpha3 of CAPI even though behavior diagram does not reflect it.
		// This will be automatically requeued when the tinkerbellCluster is unpaused.
		t.Run("tinkerbellcluster_is_paused", clusterReconciliationIsNotRequeuedWhenTinkerbellClusterIsPaused)

		// This is introduced in v1alpha3 of CAPI even though behavior diagram does not reflect it.
		// Requeue happens through watch of Cluster.
		t.Run("cluster_is_paused", clusterReconciliationIsNotRequeuedWhenClusterIsPaused)

		// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior.
		// This will be automatically requeued when the ownerRef is set.
		t.Run("cluster_has_no_owner_set", clusterReconciliationIsNotRequeuedWhenClusterHasNoOwnerSet)

		// If reconciliation process started, but we cannot find cluster object anymore, it means object has been
		// removed in the meanwhile. This means there is nothing to do.
		t.Run("cluster_object_is_missing", clusterReconciliationIsNotRequeuedWhenClusterObjectIsMissing)
	})

	t.Run("fails_when", func(t *testing.T) {
		t.Parallel()

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
	g := NewWithT(t)

	now := metav1.Now()

	tinkerbellClusterScheduledForRemoval := validTinkerbellCluster(clusterName, clusterNamespace)
	tinkerbellClusterScheduledForRemoval.DeletionTimestamp = &now

	occupiedHardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	occupiedHardware.ObjectMeta.Labels = map[string]string{
		controllers.ClusterNameLabel:      clusterName,
		controllers.ClusterNamespaceLabel: clusterNamespace,
	}
	occupiedHardware.ObjectMeta.Finalizers = []string{infrastructurev1.ClusterFinalizer}

	objects := []runtime.Object{
		occupiedHardware,
		validCluster(clusterName, clusterNamespace),
		tinkerbellClusterScheduledForRemoval,
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Unexpected error while reconciling")

	t.Run("marks_hardware_as_available_for_other_clusters_by", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		updatedHardware := &tinkv1.Hardware{}

		namespacedName := types.NamespacedName{
			Name: hardwareName,
		}

		g.Expect(client.Get(context.Background(), namespacedName, updatedHardware)).To(Succeed())

		t.Run("removing_cluster_labels", func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			g.Expect(updatedHardware.ObjectMeta.Labels).NotTo(HaveKey(controllers.ClusterNameLabel),
				"Cluster name label has not been removed from Hardware object")

			g.Expect(updatedHardware.ObjectMeta.Labels).NotTo(HaveKey(controllers.ClusterNamespaceLabel),
				"Cluster namespace label has not been removed from Hardware object")
		})

		t.Run("removing_tinkerbell_cluster_finalizer", func(t *testing.T) {
			t.Parallel()

			g.Expect(updatedHardware.ObjectMeta.Finalizers).To(BeEmpty(), "Expected all finalizers to be removed")
		})
	})

	// This makes sure that Cluster object can be gracefully removed.
	//
	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#behavior
	t.Run("removes_tinkerbell_finalizer_from_cluster_object", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

		namespacedName := types.NamespacedName{
			Name:      clusterName,
			Namespace: clusterNamespace,
		}

		g.Eventually(
			client.Get(context.Background(), namespacedName, updatedTinkerbellCluster),
		).Should(MatchError(ContainSubstring("not found")))
	})
}

// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#behavior
func Test_Cluster_reconciliation_when_cluster_is_scheduled_for_removal_it_removes_finalizer_for_cluster_without_owner(t *testing.T) { //nolint:lll
	t.Parallel()
	g := NewWithT(t)

	now := metav1.Now()

	clusterScheduledForRemovalWithoutOwner := validTinkerbellCluster(clusterName, clusterNamespace)
	clusterScheduledForRemovalWithoutOwner.ObjectMeta.DeletionTimestamp = &now
	clusterScheduledForRemovalWithoutOwner.ObjectMeta.OwnerReferences = nil

	occupiedHardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	occupiedHardware.ObjectMeta.Labels = map[string]string{
		controllers.ClusterNameLabel:      clusterName,
		controllers.ClusterNamespaceLabel: clusterNamespace,
	}
	occupiedHardware.ObjectMeta.Finalizers = []string{infrastructurev1.ClusterFinalizer}

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		clusterScheduledForRemovalWithoutOwner,
		occupiedHardware,
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Unexpected error while reconciling")

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	g.Eventually(client.Get(context.Background(), namespacedName, updatedTinkerbellCluster)).
		Should(MatchError(ContainSubstring("not found")))
}

// If removal process is interrupted at the moment we released the hardware, but we didn't get a chance to patch the
// cluster object to remove the finalizer. This prevents removal from getting stuck while interrupted.
func Test_Cluster_reconciliation_when_cluster_is_scheduled_for_removal_it_removes_finalizer_for_cluster_without_hardware(t *testing.T) { //nolint:lll
	t.Parallel()
	g := NewWithT(t)

	now := metav1.Now()

	clusterScheduledForRemoval := validTinkerbellCluster(clusterName, clusterNamespace)
	clusterScheduledForRemoval.ObjectMeta.DeletionTimestamp = &now

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		clusterScheduledForRemoval,
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Unexpected error while reconciling")

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	g.Eventually(client.Get(context.Background(), namespacedName, updatedTinkerbellCluster)).
		Should(MatchError(ContainSubstring("not found")))
}

func clusterReconciliationFailsWhenReconcilerHasNoClientSet(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	clusterController := &controllers.TinkerbellClusterReconciler{}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}

	_, err := clusterController.Reconcile(context.TODO(), request)
	g.Expect(err).To(MatchError(controllers.ErrMissingClient))
}

func clusterReconciliationFailsWhenSelectedhardwareHasNoInterfacesConfigured(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces = []tinkv1.Interface{}

	_, err := reconcileWithHardwares(t, []*tinkv1.Hardware{hardware})
	g.Expect(err).To(MatchError(controllers.ErrHardwareMissingInterfaces))
}

func clusterReconciliationFailsWhenSelectedhardwareHasNoDHCPConfiguredOnFirstInterface(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces[0].DHCP = nil

	_, err := reconcileWithHardwares(t, []*tinkv1.Hardware{hardware})
	g.Expect(err).To(MatchError(controllers.ErrHardwareFirstInterfaceNotDHCP))
}

func clusterReconciliationFailsWhenSelectedhardwareHasNoDHCPIPConfiguredOnFirstInterface(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces[0].DHCP.IP = nil

	_, err := reconcileWithHardwares(t, []*tinkv1.Hardware{hardware})
	g.Expect(err).To(MatchError(controllers.ErrHardwareFirstInterfaceDHCPMissingIP))
}

func clusterReconciliationFailsWhenSelectedhardwareHasEmptyDHCPIPConfiguredOnFirstInterface(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)
	hardware.Status.Interfaces[0].DHCP.IP.Address = ""

	_, err := reconcileWithHardwares(t, []*tinkv1.Hardware{hardware})
	g.Expect(err).To(MatchError(controllers.ErrHardwareFirstInterfaceDHCPMissingIP))
}

func kubernetesClientWithObjects(t *testing.T, objects []runtime.Object) client.Client {
	t.Helper()
	g := NewWithT(t)

	scheme := runtime.NewScheme()

	g.Expect(tinkv1.AddToScheme(scheme)).To(Succeed(), "Adding Tinkerbell objects to scheme should succeed")
	g.Expect(infrastructurev1.AddToScheme(scheme)).To(Succeed(), "Adding Tinkerbell CAPI objects to scheme should succeed")
	g.Expect(clusterv1.AddToScheme(scheme)).To(Succeed(), "Adding CAPI objects to scheme should succeed")
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed(), "Adding Core V1 objects to scheme should succeed")

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
}

//nolint:unparam
func reconcileClusterWithClient(client client.Client, name, namespace string) (ctrl.Result, error) {
	clusterController := &controllers.TinkerbellClusterReconciler{
		Client: client,
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	return clusterController.Reconcile(context.TODO(), request) //nolint:wrapcheck
}

func clusterReconciliationIsNotRequeuedWhenClusterObjectIsMissing(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, nil), clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling when cluster object does not exist should not return error")
	g.Expect(result.IsZero()).To(BeTrue(), "Expected result to not request requeue")
}

const (
	clusterName      = "myClusterName"
	clusterNamespace = "myClusterNamespace"
	hardwareIP       = "1.1.1.1"
	hardwareName     = "myHardwareName"
)

func clusterReconciliationFailsWhenThereIsNoHardwareAvailable(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	_, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	g.Expect(err).To(MatchError(controllers.ErrNoHardwareAvailable),
		"Reconciling new cluster object should fail when there is no hardware available")
}

func clusterReconciliationIsNotRequeuedWhenClusterHasNoOwnerSet(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	unreadyTinkerbellClusterWithoutOwner := unreadyTinkerbellCluster(clusterName, clusterNamespace)
	unreadyTinkerbellClusterWithoutOwner.ObjectMeta.OwnerReferences = nil

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellClusterWithoutOwner,
	}

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling new cluster object should not fail when cluster has no owner set yet")

	g.Expect(result.IsZero()).To(BeTrue(), "Expected result to not request requeue")
}

func clusterReconciliationIsNotRequeuedWhenTinkerbellClusterIsPaused(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	pausedTinkerbellCluster := validTinkerbellCluster(clusterName, clusterNamespace)
	pausedTinkerbellCluster.ObjectMeta.Annotations = map[string]string{
		clusterv1.PausedAnnotation: "true",
	}

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		pausedTinkerbellCluster,
	}

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling new cluster object should not fail when tinkerbellCluster is paused")

	g.Expect(result.IsZero()).To(BeTrue(), "Expected result to not request requeue")
}

func clusterReconciliationIsNotRequeuedWhenClusterIsPaused(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	pausedCluster := validCluster(clusterName, clusterNamespace)
	pausedCluster.Spec.Paused = true

	objects := []runtime.Object{
		pausedCluster,
		validTinkerbellCluster(clusterName, clusterNamespace),
	}

	result, err := reconcileClusterWithClient(kubernetesClientWithObjects(t, objects), clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling new cluster object should not fail when tinkerbellCluster is paused")

	g.Expect(result.IsZero()).To(BeTrue(), "Expected result to not request requeue")
}

func reconcileWithHardwares(t *testing.T, hardwares []*tinkv1.Hardware) (ctrl.Result, error) {
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
	g := NewWithT(t)

	hardware := validHardware(hardwareName, uuid.New().String(), hardwareIP)

	hardware.ObjectMeta.Labels = map[string]string{
		controllers.HardwareOwnerNameLabel:      "foo",
		controllers.HardwareOwnerNamespaceLabel: "bar",
	}

	_, err := reconcileWithHardwares(t, []*tinkv1.Hardware{hardware})
	g.Expect(err).To(MatchError(controllers.ErrNoHardwareAvailable))
}

// When multiple clusters are created simultaneously, each should get different IP address.
func clusterReconciliationAssignsUniqueControlplaneEndpointForEachCluster(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

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

	_, err := reconcileClusterWithClient(client, firstClusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling with available hardware should succeed")

	_, err = reconcileClusterWithClient(client, secondClusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling with available hardware should succeed")

	clusterNamespacedName := types.NamespacedName{
		Name:      firstClusterName,
		Namespace: clusterNamespace,
	}

	firstUpdatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	g.Expect(client.Get(context.Background(), clusterNamespacedName, firstUpdatedTinkerbellCluster)).To(Succeed(),
		"Getting first updated Tinkerbell Cluster object")

	secondUpdatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	clusterNamespacedName.Name = secondClusterName

	g.Expect(client.Get(context.Background(), clusterNamespacedName, secondUpdatedTinkerbellCluster)).To(Succeed(),
		"Getting second updated Tinkerbell Cluster object")

	firstHost := firstUpdatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host
	secondHost := secondUpdatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host

	g.Expect(firstHost).NotTo(BeEquivalentTo(secondHost), "Each cluster should get unique IP address")
}
