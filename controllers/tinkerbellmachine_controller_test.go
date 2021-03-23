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
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake" //nolint:staticcheck

	infrastructurev1alpha3 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controllers"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

func notImplemented(t *testing.T) {
	t.Helper()

	// t.Fatalf("not implemented")
	t.Skip("not implemented")
}

//nolint:unparam
func validTinkerbellMachine(name, namespace, machineName, hardwareUUID string) *infrastructurev1alpha3.TinkerbellMachine { //nolint:lll
	return &infrastructurev1alpha3.TinkerbellMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(hardwareUUID),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "cluster.x-k8s.io/v1alpha3",
					Kind:       "Machine",
					Name:       machineName,
					UID:        types.UID(hardwareUUID),
				},
			},
		},
	}
}

//nolint:unparam
func validCluster(name, namespace string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: &corev1.ObjectReference{
				Name: name,
			},
		},
	}
}

//nolint:unparam
func validTinkerbellCluster(name, namespace string) *infrastructurev1alpha3.TinkerbellCluster {
	return &infrastructurev1alpha3.TinkerbellCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{infrastructurev1alpha3.ClusterFinalizer},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "cluster.x-k8s.io/v1alpha3",
					Kind:       "Cluster",
					Name:       name,
				},
			},
		},
		Spec: infrastructurev1alpha3.TinkerbellClusterSpec{
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: hardwareIP,
				Port: controllers.KubernetesAPIPort,
			},
		},
		Status: infrastructurev1alpha3.TinkerbellClusterStatus{
			Ready: true,
		},
	}
}

//nolint:unparam
func validMachine(name, namespace, clusterName string) *clusterv1.Machine {
	return &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: clusterName,
			},
		},
		Spec: clusterv1.MachineSpec{
			Version: pointer.StringPtr("1.19.4"),
			Bootstrap: clusterv1.Bootstrap{
				DataSecretName: pointer.StringPtr(name),
			},
		},
	}
}

//nolint:unparam
func validSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"value": []byte("not nil bootstrap data"),
		},
	}
}

func validHardware(name, uuid, ip string) *tinkv1alpha1.Hardware {
	return &tinkv1alpha1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: tinkv1alpha1.HardwareSpec{
			ID: uuid,
		},
		Status: tinkv1alpha1.HardwareStatus{
			Interfaces: []tinkv1alpha1.Interface{
				{
					DHCP: &tinkv1alpha1.DHCP{
						IP: &tinkv1alpha1.IP{
							Address: ip,
						},
					},
				},
			},
		},
	}
}

//nolint:funlen,gocognit,cyclop
func Test_Machine_reconciliation_with_available_hardware(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	ctx := context.Background()

	globalResourceName := types.NamespacedName{
		Name: tinkerbellMachineName,
	}

	t.Run("creates_template", func(t *testing.T) {
		t.Parallel()

		template := &tinkv1alpha1.Template{}

		if err := client.Get(ctx, globalResourceName, template); err != nil {
			t.Fatalf("Expected template to be created, got: %v", err)
		}

		// Owner reference is required to make use of Kubernetes GC for removing dependent objects, so if
		// machine gets force-removed, template will be cleaned up.
		t.Run("with_owner_reference_set", func(t *testing.T) {
			if len(template.ObjectMeta.OwnerReferences) == 0 {
				t.Fatalf("Expected at least one owner reference to be set")
			}

			if uid := template.ObjectMeta.OwnerReferences[0].UID; uid != types.UID(hardwareUUID) {
				t.Fatalf("Expected owner reference UID to be %v, got %v", types.UID(hardwareUUID), uid)
			}
		})
	})

	t.Run("creates_workflow", func(t *testing.T) {
		t.Parallel()

		workflow := &tinkv1alpha1.Workflow{}

		if err := client.Get(ctx, globalResourceName, workflow); err != nil {
			t.Fatalf("Expected workflow to be created, got: %v", err)
		}

		// Owner reference is required to make use of Kubernetes GC for removing dependent objects, so if
		// machine gets force-removed, workflow will be cleaned up.
		t.Run("with_owner_reference_set", func(t *testing.T) {
			if len(workflow.ObjectMeta.OwnerReferences) == 0 {
				t.Fatalf("Expected at least one owner reference to be set")
			}

			if name := workflow.ObjectMeta.OwnerReferences[0].Name; name != tinkerbellMachineName {
				t.Fatalf("Expected owner reference name to be %q, got %q", tinkerbellMachineName, name)
			}
		})
	})

	namespacedName := types.NamespacedName{
		Name:      tinkerbellMachineName,
		Namespace: clusterNamespace,
	}

	updatedMachine := &infrastructurev1alpha3.TinkerbellMachine{}
	if err := client.Get(ctx, namespacedName, updatedMachine); err != nil {
		t.Fatalf("Getting updated hardware: %v", err)
	}

	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#normal-resource.
	t.Run("sets_provider_id_with_selected_hardware_id", func(t *testing.T) {
		t.Parallel()

		if !strings.Contains(updatedMachine.Spec.ProviderID, hardwareUUID) {
			t.Fatalf("Expected ProviderID field to include %q, got %q", hardwareUUID, updatedMachine.Spec.ProviderID)
		}
	})

	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#normal-resource.
	t.Run("sets_tinkerbell_machine_status_to_ready", func(t *testing.T) {
		t.Parallel()

		if !updatedMachine.Status.Ready {
			t.Fatalf("Machine is not ready")
		}
	})

	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#normal-resource.
	t.Run("sets_tinkerbell_finalizer", func(t *testing.T) {
		t.Parallel()

		if len(updatedMachine.ObjectMeta.Finalizers) == 0 {
			t.Fatalf("Expected at least one finalizer to be set")
		}
	})

	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#normal-resource.
	t.Run("sets_tinkerbell_machine_IP_address", func(t *testing.T) {
		t.Parallel()

		if len(updatedMachine.Status.Addresses) == 0 {
			t.Fatalf("Expected at least one IP address to be populated")
		}

		if updatedMachine.Status.Addresses[0].Address != hardwareIP {
			t.Fatalf("Expected first IP address to be %q, got %q", hardwareIP, updatedMachine.Status.Addresses[0].Address)
		}
	})

	// So it becomes unavailable for other clusters.
	t.Run("sets_ownership_label_on_selected_hardware", func(t *testing.T) {
		t.Parallel()

		hardwareNamespacedName := types.NamespacedName{
			Name: hardwareName,
		}

		updatedHardware := &tinkv1alpha1.Hardware{}
		if err := client.Get(ctx, hardwareNamespacedName, updatedHardware); err != nil {
			t.Fatalf("Getting updated hardware: %v", err)
		}

		ownerName, ok := updatedHardware.ObjectMeta.Labels[controllers.HardwareOwnerNameLabel]
		if !ok {
			t.Fatalf("Owner name label missing on selected hardware")
		}

		ownerNamespace, ok := updatedHardware.ObjectMeta.Labels[controllers.HardwareOwnerNamespaceLabel]
		if !ok {
			t.Fatalf("Owner namespace label missing on selected hardware")
		}

		if ownerName != tinkerbellMachineName {
			t.Errorf("Expected owner name %q, got %q", tinkerbellMachineName, ownerName)
		}

		if ownerNamespace != clusterNamespace {
			t.Errorf("Expected owner namespace %q, got %q", clusterNamespace, ownerNamespace)
		}
	})

	// Ensure idempotency of reconcile operation. E.g. we shouldn't try to create the template with the same name
	// on every iteration.
	t.Run("succeeds_when_executed_twice", func(t *testing.T) {
		t.Parallel()

		if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
			t.Fatalf("Unexpected reconciliation error: %v", err)
		}
	})

	// Status should be updated on every run.
	//
	// Don't execute this test in parallel, as we reset status here.
	t.Run("refreshes_status_when_machine_is_already_provisioned", func(t *testing.T) { //nolint:paralleltest
		updatedMachine.Status.Addresses = nil

		if err := client.Update(context.Background(), updatedMachine); err != nil {
			t.Fatalf("Updating machine: %v", err)
		}

		if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
			t.Fatalf("Unexpected reconciliation error: %v", err)
		}

		updatedMachine = &infrastructurev1alpha3.TinkerbellMachine{}
		if err := client.Get(ctx, namespacedName, updatedMachine); err != nil {
			t.Fatalf("Getting updated hardware: %v", err)
		}

		if len(updatedMachine.Status.Addresses) == 0 {
			t.Fatalf("Machine status should be updated on every reconciliation")
		}
	})
}

//nolint:funlen
func Test_Machine_reconciliation(t *testing.T) {
	t.Parallel()

	t.Run("is_requeued_when", func(t *testing.T) {
		t.Parallel()

		t.Run("is_requeued_when_machine_object_is_missing",
			machineReconciliationIsRequeuedWhenMachineObjectIsMissing)

		// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior
		t.Run("machine_has_no_owner_set", machineReconciliationIsRequeuedWhenMachineHasNoOwnerSet)

		// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior
		t.Run("bootstrap_secret_is_not_ready", machineReconciliationIsRequeuedWhenBootstrapSecretIsNotReady)

		// From https://cluster-api.sigs.k8s.io/developer/providers/cluster-infrastructure.html#behavior
		t.Run("cluster_infrastructure_is_not_ready", machineReconciliationIsRequeuedWhenClusterInfrastructureIsNotReady)
	})

	t.Run("fails_when", func(t *testing.T) {
		t.Parallel()

		t.Run("reconciler_is_nil", machineReconciliationFailsWhenReconcilerIsNil)
		t.Run("reconciler_has_no_logger_set", machineReconciliationFailsWhenReconcilerHasNoLoggerSet)
		t.Run("reconciler_has_no_client_set", machineReconciliationFailsWhenReconcilerHasNoClientSet)

		// CAPI spec says this is optional, but @detiber says it's effectively required, so treat it as so.
		t.Run("machine_has_no_version_set", machineReconciliationFailsWhenMachineHasNoVersionSet)

		t.Run("associated_cluster_object_does_not_exist",
			machineReconciliationFailsWhenAssociatedClusterObjectDoesNotExist)

		t.Run("associated_tinkerbell_cluster_object_does_not_exist",
			machineReconciliationFailsWhenAssociatedTinkerbellClusterObjectDoesNotExist)

		// If for example CAPI changes key used to store bootstrap date, we shouldn't try to create machines
		// with empty bootstrap config, we should fail early instead.
		t.Run("bootstrap_config_is_empty", machineReconciliationFailsWhenBootstrapConfigIsEmpty)
		t.Run("bootstrap_config_has_no_value_key", machineReconciliationFailsWhenBootstrapConfigHasNoValueKey)

		t.Run("there_is_no_hardware_available", machineReconciliationFailsWhenThereIsNoHardwareAvailable)

		t.Run("selected_hardware_has_no_ip_address_set", machineReconciliationFailsWhenSelectedHardwareHasNoIPAddressSet)

		// We only support single controlplane node at the moment as we don't have a concept of load balancer, so it's
		// the role of cluster controller to select which hardware to use for controlplane node.
		t.Run("reconciling_controlplane_machine_and_there_is_no_hardware_available_labeled_by_cluster_controller",
			machineReconciliationFailsWhenReconcilingControlplaneMachineAndThereIsNoHardwareAavailableLabeledByClusterController)
	})

	// We only support single controlplane node at the moment as we don't have a concept of load balancer, so it's
	// the role of cluster controller to select which hardware to use for controlplane node.
	t.Run("uses_hardware_selected_by_cluster_controller_for_controlplane_node", //nolint:paralleltest
		machineReconciliationUsesHardwareSelectedByClusterControllerForControlplaneNode)

	// Single hardware should only ever be used for a single machine.
	t.Run("selects_unique_and_available_hardware_for_each_machine", //nolint:paralleltest
		machineReconciliationSelectsUniqueAndAvailablehardwareForEachMachine)

	// Patching Hardware and TinkerbellMachine are not atomic operations, so we should handle situation, when
	// misspelling process is aborted in the middle.
	//
	// Without that, new Hardware will be selected each time.
	t.Run("uses_already_selected_hardware_if_patching_tinkerbell_machine_failed", //nolint:paralleltest
		machineReconciliationUsesAlreadySelectedHardwareIfPatchingTinkerbellMachineFailed)

	t.Run("when_machine_is_scheduled_for_removal_it", func(t *testing.T) {
		t.Parallel()

		// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#behavior
		t.Run("removes_tinkerbell_finalizer", notImplemented)

		// Removing machine should release used hardware.
		t.Run("marks_hardware_as_available_for_other_machines", notImplemented)
	})
}

//nolint:funlen
func Test_Machine_reconciliation_when_machine_is_scheduled_for_removal_it(t *testing.T) {
	t.Parallel()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, ""),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	ctx := context.Background()

	tinkerbellMachineNamespacedName := types.NamespacedName{
		Name:      tinkerbellMachineName,
		Namespace: clusterNamespace,
	}

	updatedMachine := &infrastructurev1alpha3.TinkerbellMachine{}
	if err := client.Get(ctx, tinkerbellMachineNamespacedName, updatedMachine); err != nil {
		t.Fatalf("Getting updated machine: %v", err)
	}

	now := metav1.Now()

	updatedMachine.ObjectMeta.DeletionTimestamp = &now

	if err := client.Update(ctx, updatedMachine); err != nil {
		t.Fatalf("Updating machine: %v", err)
	}

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	hardwareNamespacedName := types.NamespacedName{
		Name: hardwareName,
	}

	updatedHardware := &tinkv1alpha1.Hardware{}
	if err := client.Get(ctx, hardwareNamespacedName, updatedHardware); err != nil {
		t.Fatalf("Getting updated hardware: %v", err)
	}

	t.Run("removes_tinkerbell_machine_finalizer_from_hardware", func(t *testing.T) {
		t.Parallel()

		if len(updatedHardware.ObjectMeta.Finalizers) != 0 {
			t.Errorf("Unexpected finalizers: %v", updatedHardware.ObjectMeta.Finalizers)
		}
	})

	t.Run("makes_hardware_available_for_other_machines", func(t *testing.T) {
		t.Parallel()

		if _, ok := updatedHardware.ObjectMeta.Labels[controllers.HardwareOwnerNameLabel]; ok {
			t.Errorf("Found hardware owner name label")
		}

		if _, ok := updatedHardware.ObjectMeta.Labels[controllers.HardwareOwnerNamespaceLabel]; ok {
			t.Errorf("Found hardware owner namespace label")
		}
	})
}

const (
	machineName           = "myMachineName"
	tinkerbellMachineName = "myTinkerbellMachineName"
)

func machineReconciliationFailsWhenReconcilerIsNil(t *testing.T) {
	t.Parallel()

	var machineController *controllers.TinkerbellMachineReconciler

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      tinkerbellMachineName,
		},
	}

	if _, err := machineController.Reconcile(request); err == nil {
		t.Fatalf("Expected error while reconciling")
	}
}

func machineReconciliationFailsWhenReconcilerHasNoLoggerSet(t *testing.T) {
	t.Parallel()

	machineController := &controllers.TinkerbellMachineReconciler{
		Log: logr.Discard(),
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      tinkerbellMachineName,
		},
	}

	if _, err := machineController.Reconcile(request); err == nil {
		t.Fatalf("Expected error while reconciling")
	}
}

func machineReconciliationFailsWhenReconcilerHasNoClientSet(t *testing.T) {
	t.Parallel()

	machineController := &controllers.TinkerbellMachineReconciler{
		Client: fake.NewFakeClient(),
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      tinkerbellMachineName,
		},
	}

	if _, err := machineController.Reconcile(request); err == nil {
		t.Fatalf("Expected error while reconciling")
	}
}

//nolint:unparam
func reconcileMachineWithClient(client client.Client, name, namespace string) (ctrl.Result, error) {
	log := logr.Discard()

	machineController := &controllers.TinkerbellMachineReconciler{
		Log:    log,
		Client: client,
	}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	return machineController.Reconcile(request)
}

func machineReconciliationIsRequeuedWhenMachineObjectIsMissing(t *testing.T) {
	t.Parallel()

	result, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, nil), tinkerbellMachineName, clusterNamespace)
	if err != nil {
		t.Fatalf("Reconciling when machine object does not exist should not return error")
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("Expected non zero requeue time")
	}
}

func machineReconciliationIsRequeuedWhenMachineHasNoOwnerSet(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	tinkerbellMachine := validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID)

	tinkerbellMachine.ObjectMeta.OwnerReferences = nil

	objects := []runtime.Object{
		tinkerbellMachine,
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	result, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace)
	if err != nil {
		t.Fatalf("Reconciling when machine object does not exist should not return error")
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("Expected non zero requeue time")
	}
}

func machineReconciliationIsRequeuedWhenBootstrapSecretIsNotReady(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	machineWithoutSecretReference := validMachine(machineName, clusterNamespace, clusterName)
	machineWithoutSecretReference.Spec.Bootstrap.DataSecretName = nil

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		machineWithoutSecretReference,
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	result, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace)
	if err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("Expected non-zero requeue time")
	}
}

func machineReconciliationIsRequeuedWhenClusterInfrastructureIsNotReady(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	notReadyTinkerbellCluster := validTinkerbellCluster(clusterName, clusterNamespace)
	notReadyTinkerbellCluster.Status.Ready = false

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		notReadyTinkerbellCluster,
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	result, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace)
	if err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	if result.RequeueAfter == 0 {
		t.Fatalf("Expected reconciliation to be requeued")
	}
}

func machineReconciliationFailsWhenMachineHasNoVersionSet(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	machineWithoutVersion := validMachine(machineName, clusterNamespace, clusterName)
	machineWithoutVersion.Spec.Version = nil

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		machineWithoutVersion,
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err == nil {
		t.Fatalf("Reconciling when owner machine has no version set should fail")
	}
}

func machineReconciliationFailsWhenBootstrapConfigIsEmpty(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	emptySecret := validSecret(machineName, clusterNamespace)
	emptySecret.Data["value"] = nil

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		emptySecret,
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when owner machine has no version set should fail")
	}

	expectedError := "received bootstrap cloud config is empty"
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationFailsWhenBootstrapConfigHasNoValueKey(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	emptySecret := validSecret(machineName, clusterNamespace)
	emptySecret.Data = nil

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		emptySecret,
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when bootstrap config has no expected key should fail")
	}

	expectedError := "secret value key is missing"
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationFailsWhenAssociatedClusterObjectDoesNotExist(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		// validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when owner machine has no version set should fail")
	}

	if expectedError := "not found"; !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationFailsWhenAssociatedTinkerbellClusterObjectDoesNotExist(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		// validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when owner machine has no version set should fail")
	}

	expectedError := "getting TinkerbellCluster object"
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationFailsWhenThereIsNoHardwareAvailable(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		// validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when there is no hardware available should fail")
	}

	expectedError := "no hardware available"
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationFailsWhenReconcilingControlplaneMachineAndThereIsNoHardwareAavailableLabeledByClusterController(t *testing.T) { //nolint:lll
	t.Parallel()

	hardwareUUID := uuid.New().String()

	controlplaneMachine := validMachine(machineName, clusterNamespace, clusterName)
	controlplaneMachine.Labels[clusterv1.MachineControlPlaneLabelName] = "true"

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		controlplaneMachine,
		validSecret(machineName, clusterNamespace),
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when owner machine has no version set should fail")
	}

	expectedError := "no hardware available"
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationFailsWhenSelectedHardwareHasNoIPAddressSet(t *testing.T) {
	t.Parallel()

	hardwareUUID := uuid.New().String()

	malformedHardware := validHardware(hardwareName, hardwareUUID, "")

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		malformedHardware,
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	_, err := reconcileMachineWithClient(kubernetesClientWithObjects(t, objects), tinkerbellMachineName, clusterNamespace)
	if err == nil {
		t.Fatalf("Reconciling when owner machine has no version set should fail")
	}

	expectedError := "address is empty"
	if !strings.Contains(err.Error(), expectedError) {
		t.Fatalf("Unexpected error. Expected %q, got: %v", expectedError, err)
	}
}

func machineReconciliationUsesHardwareSelectedByClusterControllerForControlplaneNode(t *testing.T) {
	t.Parallel()

	controlplaneHardwareUUID := uuid.New().String()

	controlplaneHardware := validHardware("myhardware", controlplaneHardwareUUID, "2.2.2.2")

	controlplaneHardware.ObjectMeta.Labels = map[string]string{
		controllers.ClusterNameLabel:      clusterName,
		controllers.ClusterNamespaceLabel: clusterNamespace,
	}

	controlplaneMachine := validMachine(machineName, clusterNamespace, clusterName)
	controlplaneMachine.Labels[clusterv1.MachineControlPlaneLabelName] = "true"

	hardwareUUID := uuid.New().String()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		controlplaneHardware,
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		controlplaneMachine,
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	ctx := context.Background()

	namespacedName := types.NamespacedName{
		Name:      tinkerbellMachineName,
		Namespace: clusterNamespace,
	}

	updatedMachine := &infrastructurev1alpha3.TinkerbellMachine{}
	if err := client.Get(ctx, namespacedName, updatedMachine); err != nil {
		t.Fatalf("Getting updated hardware: %v", err)
	}

	// From https://cluster-api.sigs.k8s.io/developer/providers/machine-infrastructure.html#normal-resource.
	if !strings.Contains(updatedMachine.Spec.ProviderID, controlplaneHardwareUUID) {
		t.Fatalf("Expected ProviderID field to include %q, got %q",
			controlplaneHardwareUUID, updatedMachine.Spec.ProviderID)
	}
}

func machineReconciliationSelectsUniqueAndAvailablehardwareForEachMachine(t *testing.T) {
	t.Parallel()

	secondMachineName := "secondMachineName"
	secondHardwareName := "secondHardwareName"

	firstHardwareUUID := uuid.New().String()
	secondHardwareUUID := uuid.New().String()

	secondTinkerbellMachineName := "secondTinkerbellMachineName"

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, firstHardwareUUID),
		validTinkerbellMachine(secondTinkerbellMachineName, clusterNamespace, secondMachineName, secondHardwareUUID),

		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),

		validHardware(hardwareName, firstHardwareUUID, hardwareIP),
		validHardware(secondHardwareName, secondHardwareUUID, "2.2.2.2"),

		validMachine(machineName, clusterNamespace, clusterName),
		validMachine(secondMachineName, clusterNamespace, clusterName),

		validSecret(machineName, clusterNamespace),
		validSecret(secondMachineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	tinkerbellMachineNamespacedName := types.NamespacedName{
		Name:      tinkerbellMachineName,
		Namespace: clusterNamespace,
	}

	ctx := context.Background()

	firstMachine := &infrastructurev1alpha3.TinkerbellMachine{}
	if err := client.Get(ctx, tinkerbellMachineNamespacedName, firstMachine); err != nil {
		t.Fatalf("Getting first updated machine: %v", err)
	}

	if _, err := reconcileMachineWithClient(client, secondTinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error of 2nd machine: %v", err)
	}

	tinkerbellMachineNamespacedName.Name = secondTinkerbellMachineName

	secondMachine := &infrastructurev1alpha3.TinkerbellMachine{}
	if err := client.Get(ctx, tinkerbellMachineNamespacedName, secondMachine); err != nil {
		t.Fatalf("Getting second updated machine: %v", err)
	}

	if firstMachine.Spec.HardwareName == secondMachine.Spec.HardwareName {
		t.Fatalf("Two machines use the same hardware %q", firstMachine.Spec.HardwareName)
	}
}

func machineReconciliationUsesAlreadySelectedHardwareIfPatchingTinkerbellMachineFailed(t *testing.T) {
	t.Parallel()

	expectedHardwareName := "alreadyOwnedHardware"
	alreadyOwnedHardware := validHardware(expectedHardwareName, uuid.New().String(), "2.2.2.2")
	alreadyOwnedHardware.ObjectMeta.Labels = map[string]string{
		controllers.HardwareOwnerNameLabel:      tinkerbellMachineName,
		controllers.HardwareOwnerNamespaceLabel: clusterNamespace,
	}

	hardwareUUID := uuid.New().String()

	objects := []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		alreadyOwnedHardware,
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	if _, err := reconcileMachineWithClient(client, tinkerbellMachineName, clusterNamespace); err != nil {
		t.Fatalf("Unexpected reconciliation error: %v", err)
	}

	ctx := context.Background()

	tinkerbellMachineNamespacedName := types.NamespacedName{
		Name:      tinkerbellMachineName,
		Namespace: clusterNamespace,
	}

	updatedMachine := &infrastructurev1alpha3.TinkerbellMachine{}
	if err := client.Get(ctx, tinkerbellMachineNamespacedName, updatedMachine); err != nil {
		t.Fatalf("Getting updated machine: %v", err)
	}

	if updatedMachine.Spec.HardwareName != expectedHardwareName {
		t.Fatalf("Wrong hardware selected. Expected %q, got %q", expectedHardwareName, updatedMachine.Spec.HardwareName)
	}
}
