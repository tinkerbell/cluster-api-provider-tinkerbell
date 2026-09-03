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

package cluster_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controller"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controller/cluster"
)

//nolint:unparam
func unreadyTinkerbellCluster(name, namespace string) *infrastructurev1.TinkerbellCluster {
	unreadyTinkerbellCluster := validTinkerbellCluster(name, namespace)
	unreadyTinkerbellCluster.Status.Ready = false
	unreadyTinkerbellCluster.Finalizers = nil
	unreadyTinkerbellCluster.Spec.ControlPlaneEndpoint = nil

	return unreadyTinkerbellCluster
}

func Test_Cluster_reconciliation_when_controlplane_endpoint_not_set(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	objects := []runtime.Object{
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
		validCluster(clusterName, clusterNamespace),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).To(MatchError(cluster.ErrControlPlaneEndpointNotSet))
}

func Test_Cluster_reconciliation_when_controlplane_endpoint_set_on_cluster(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cluster := validCluster(clusterName, clusterNamespace)
	cluster.Spec.ControlPlaneEndpoint.Host = "192.168.1.10"
	cluster.Spec.ControlPlaneEndpoint.Port = 443

	objects := []runtime.Object{
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
		cluster.DeepCopy(),
		unreadyTinkerbellCluster(clusterName, clusterNamespace),
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	g.Expect(client.Get(context.Background(), namespacedName, updatedTinkerbellCluster)).To(Succeed())

	g.Expect(updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host).
		To(BeEquivalentTo(cluster.Spec.ControlPlaneEndpoint.Host), "Expected controlplane endpoint host to be set")

	g.Expect(updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Port).
		To(BeEquivalentTo(cluster.Spec.ControlPlaneEndpoint.Port), "Expected controlplane endpoint port to be set")

	g.Expect(updatedTinkerbellCluster.Status.Ready).To(BeTrue(), "Expected infrastructure to be ready")
	g.Expect(updatedTinkerbellCluster.Status.Initialization).NotTo(BeNil(), "Expected initialization status to be set")
	g.Expect(updatedTinkerbellCluster.Status.Initialization.Provisioned).NotTo(BeNil(), "Expected initialization.provisioned to be set")
	g.Expect(*updatedTinkerbellCluster.Status.Initialization.Provisioned).To(BeTrue(), "Expected initialization.provisioned to be true")
}

type testOptions struct {
	// Labels allow providing labels for the machine
	Labels           map[string]string
	HardwareAffinity *infrastructurev1.HardwareAffinity
}

func validHardware(name, uuid, ip string, options ...testOptions) *tinkv1.Hardware {
	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: clusterNamespace,
			UID:       types.UID(uuid),
		},
		Spec: tinkv1.HardwareSpec{
			Disks: []tinkv1.Disk{
				{
					Device: "/dev/sda",
				},
			},
			Interfaces: []tinkv1.Interface{
				{
					DHCP: &tinkv1.DHCP{
						IP: &tinkv1.IP{
							Address: ip,
						},
					},
					Netboot: &tinkv1.Netboot{
						AllowPXE: ptr.To(true),
					},
				},
			},
			Metadata: &tinkv1.HardwareMetadata{
				Instance: &tinkv1.MetadataInstance{
					ID: ip,
				},
			},
		},
	}

	for _, o := range options {
		for k, v := range o.Labels {
			if hw.Labels == nil {
				hw.Labels = map[string]string{}
			}

			hw.Labels[k] = v
		}
	}

	return hw
}

func Test_Cluster_reconciliation_when_controlplane_endpoint_set_on_tinkerbellCluster(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	tinkCluster := unreadyTinkerbellCluster(clusterName, clusterNamespace)
	tinkCluster.Spec.ControlPlaneEndpoint = &clusterv1.APIEndpoint{Host: "192.168.1.10", Port: 443}

	objects := []runtime.Object{
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
		validCluster(clusterName, clusterNamespace),
		tinkCluster.DeepCopy(),
	}

	client := kubernetesClientWithObjects(t, objects)

	_, err := reconcileClusterWithClient(client, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	namespacedName := types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}

	g.Expect(client.Get(context.Background(), namespacedName, updatedTinkerbellCluster)).To(Succeed())

	g.Expect(updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Host).
		To(BeEquivalentTo(tinkCluster.Spec.ControlPlaneEndpoint.Host), "Expected controlplane endpoint host to be set")

	g.Expect(updatedTinkerbellCluster.Spec.ControlPlaneEndpoint.Port).
		To(BeEquivalentTo(tinkCluster.Spec.ControlPlaneEndpoint.Port), "Expected controlplane endpoint port to be set")

	g.Expect(updatedTinkerbellCluster.Status.Ready).To(BeTrue(), "Expected infrastructure to be ready")
	g.Expect(updatedTinkerbellCluster.Status.Initialization).NotTo(BeNil(), "Expected initialization status to be set")
	g.Expect(updatedTinkerbellCluster.Status.Initialization.Provisioned).NotTo(BeNil(), "Expected initialization.provisioned to be set")
	g.Expect(*updatedTinkerbellCluster.Status.Initialization.Provisioned).To(BeTrue(), "Expected initialization.provisioned to be true")
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

		// Unpausing a previously paused cluster should resume reconciliation.
		t.Run("cluster_is_unpaused", clusterReconciliationSetsNotPausedConditionWhenUnpaused)

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
	})
}

func clusterReconciliationFailsWhenReconcilerHasNoClientSet(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	clusterController := &cluster.TinkerbellClusterReconciler{}

	request := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: clusterNamespace,
			Name:      clusterName,
		},
	}

	_, err := clusterController.Reconcile(context.TODO(), request)
	g.Expect(err).To(MatchError(cluster.ErrMissingClient))
}

func kubernetesClientWithObjects(t *testing.T, objects []runtime.Object) client.Client {
	t.Helper()
	g := NewWithT(t)

	scheme := runtime.NewScheme()

	g.Expect(controller.AddToSchemeTinkerbell(scheme)).To(Succeed(), "Adding Tinkerbell objects to scheme should succeed")
	g.Expect(infrastructurev1.AddToScheme(scheme)).To(Succeed(), "Adding Tinkerbell CAPI objects to scheme should succeed")
	g.Expect(clusterv1.AddToScheme(scheme)).To(Succeed(), "Adding CAPI objects to scheme should succeed")
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed(), "Adding Core V1 objects to scheme should succeed")

	objs := []client.Object{
		&infrastructurev1.TinkerbellMachine{},
		&infrastructurev1.TinkerbellCluster{},
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).WithStatusSubresource(objs...).Build()
}

//nolint:unparam
func reconcileClusterWithClient(client client.Client, name, namespace string) (ctrl.Result, error) {
	clusterController := &cluster.TinkerbellClusterReconciler{
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

//nolint:unparam
func validCluster(name, namespace string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				Name: name,
			},
		},
	}
}

func validTinkerbellCluster(name, namespace string) *infrastructurev1.TinkerbellCluster {
	tinkCluster := &infrastructurev1.TinkerbellCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{infrastructurev1.ClusterFinalizer},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "cluster.x-k8s.io/v1beta1",
					Kind:       "Cluster",
					Name:       name,
				},
			},
		},
		Spec: infrastructurev1.TinkerbellClusterSpec{
			ControlPlaneEndpoint: &clusterv1.APIEndpoint{
				Host: hardwareIP,
				Port: 6443,
			},
		},
		Status: infrastructurev1.TinkerbellClusterStatus{
			Ready: true,
			Initialization: &infrastructurev1.TinkerbellClusterInitializationStatus{
				Provisioned: ptr.To(true),
			},
		},
	}

	_ = tinkCluster.Default(context.TODO(), tinkCluster)

	return tinkCluster
}

func clusterReconciliationIsNotRequeuedWhenClusterHasNoOwnerSet(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	unreadyTinkerbellClusterWithoutOwner := unreadyTinkerbellCluster(clusterName, clusterNamespace)
	unreadyTinkerbellClusterWithoutOwner.OwnerReferences = nil

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
	pausedTinkerbellCluster.Annotations = map[string]string{
		clusterv1.PausedAnnotation: "true",
	}

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		pausedTinkerbellCluster,
	}

	k8sClient := kubernetesClientWithObjects(t, objects)

	result, err := reconcileClusterWithClient(k8sClient, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling should not fail when tinkerbellCluster is paused")
	g.Expect(result.IsZero()).To(BeTrue(), "Expected no requeue when paused")

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}
	g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: clusterName, Namespace: clusterNamespace}, updatedTinkerbellCluster)).To(Succeed())

	pausedCondition := findCondition(updatedTinkerbellCluster.GetConditions(), clusterv1.PausedCondition)
	g.Expect(pausedCondition).NotTo(BeNil(), "Expected Paused condition to be set")
	g.Expect(pausedCondition.Status).To(Equal(metav1.ConditionTrue), "Expected Paused condition to be True")
	g.Expect(pausedCondition.Reason).To(Equal(clusterv1.PausedReason), "Expected Paused reason")
}

func clusterReconciliationIsNotRequeuedWhenClusterIsPaused(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	pausedCluster := validCluster(clusterName, clusterNamespace)
	pausedCluster.Spec.Paused = ptr.To(true)

	objects := []runtime.Object{
		pausedCluster,
		validTinkerbellCluster(clusterName, clusterNamespace),
	}

	k8sClient := kubernetesClientWithObjects(t, objects)

	result, err := reconcileClusterWithClient(k8sClient, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling should not fail when cluster is paused")
	g.Expect(result.IsZero()).To(BeTrue(), "Expected no requeue when paused")

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}
	g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: clusterName, Namespace: clusterNamespace}, updatedTinkerbellCluster)).To(Succeed())

	pausedCondition := findCondition(updatedTinkerbellCluster.GetConditions(), clusterv1.PausedCondition)
	g.Expect(pausedCondition).NotTo(BeNil(), "Expected Paused condition to be set")
	g.Expect(pausedCondition.Status).To(Equal(metav1.ConditionTrue), "Expected Paused condition to be True")
	g.Expect(pausedCondition.Reason).To(Equal(clusterv1.PausedReason), "Expected Paused reason")
}

func clusterReconciliationSetsNotPausedConditionWhenUnpaused(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	objects := []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
	}

	k8sClient := kubernetesClientWithObjects(t, objects)

	result, err := reconcileClusterWithClient(k8sClient, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling should not fail when cluster is not paused")
	g.Expect(result.IsZero()).To(BeTrue(), "Expected no requeue for normal reconciliation")

	updatedTinkerbellCluster := &infrastructurev1.TinkerbellCluster{}
	g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: clusterName, Namespace: clusterNamespace}, updatedTinkerbellCluster)).To(Succeed())

	pausedCondition := findCondition(updatedTinkerbellCluster.GetConditions(), clusterv1.PausedCondition)
	g.Expect(pausedCondition).NotTo(BeNil(), "Expected Paused condition to be set")
	g.Expect(pausedCondition.Status).To(Equal(metav1.ConditionFalse), "Expected Paused condition to be False")
	g.Expect(pausedCondition.Reason).To(Equal(clusterv1.NotPausedReason), "Expected NotPaused reason")
}

// findCondition returns the condition with the given type, or nil if not found.
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

const rackLabel = "topology.tinkerbell.org/rack"

// tinkerbellClusterWithFailureDomainLabel is validTinkerbellCluster with failure domain
// discovery switched on.
func tinkerbellClusterWithFailureDomainLabel() *infrastructurev1.TinkerbellCluster {
	tinkCluster := validTinkerbellCluster(clusterName, clusterNamespace)
	tinkCluster.Spec.FailureDomainLabel = rackLabel

	return tinkCluster
}

// reconciledFailureDomains reconciles once and returns the published failure domains
// alongside the reconcile result.
func reconciledFailureDomains(t *testing.T, objects []runtime.Object) ([]clusterv1.FailureDomain, ctrl.Result) {
	t.Helper()
	g := NewWithT(t)

	kubeClient := kubernetesClientWithObjects(t, objects)

	result, err := reconcileClusterWithClient(kubeClient, clusterName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling a valid cluster should succeed")

	updated := &infrastructurev1.TinkerbellCluster{}
	g.Expect(kubeClient.Get(context.TODO(), types.NamespacedName{
		Name:      clusterName,
		Namespace: clusterNamespace,
	}, updated)).To(Succeed(), "Getting the reconciled TinkerbellCluster should succeed")

	return updated.Status.FailureDomains, result
}

func Test_Cluster_failure_domains_are_published_from_distinct_hardware_label_values(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inRack := func(name, ip, rack string) *tinkv1.Hardware {
		return validHardware(name, uuid.New().String(), ip, testOptions{
			Labels: map[string]string{rackLabel: rack},
		})
	}

	domains, result := reconciledFailureDomains(t, []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		tinkerbellClusterWithFailureDomainLabel(),
		// Two racks, three machines: "b" appears twice and must collapse to one domain.
		inRack("hw-1", "1.1.1.1", "rack-b"),
		inRack("hw-2", "2.2.2.2", "rack-a"),
		inRack("hw-3", "3.3.3.3", "rack-b"),
	})

	g.Expect(domains).To(HaveLen(2), "Expected one failure domain per distinct label value")

	// Sorted, so that the list copied into Cluster.status.failureDomains is stable and
	// does not rewrite that status on every pass.
	g.Expect(domains[0].Name).To(Equal("rack-a"), "Expected failure domains in sorted order")
	g.Expect(domains[1].Name).To(Equal("rack-b"), "Expected failure domains in sorted order")

	// KubeadmControlPlane discards any domain that is not marked for the control plane,
	// so an unset ControlPlane would publish domains it will never spread across.
	for _, domain := range domains {
		g.Expect(domain.ControlPlane).NotTo(BeNil(), "Expected controlPlane to be set")
		g.Expect(*domain.ControlPlane).To(BeTrue(), "Expected the domain to be usable by control plane machines")
	}

	// Local mode: a Hardware watch re-triggers the reconcile, so no resync is scheduled.
	g.Expect(result.IsZero()).To(BeTrue(),
		"Expected no resync in local mode, where Hardware is watched")
}

func Test_Cluster_failure_domains_are_resynced_when_tinkerbell_is_external(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// External Hardware lives in a cluster the manager's cache cannot watch, so the only
	// way status.failureDomains tracks it is by re-reconciling on a timer.
	kubeClient := kubernetesClientWithObjects(t, []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		tinkerbellClusterWithFailureDomainLabel(),
		validHardware(hardwareName, uuid.New().String(), hardwareIP, testOptions{
			Labels: map[string]string{rackLabel: "rack-a"},
		}),
	})

	clusterController := &cluster.TinkerbellClusterReconciler{
		Client:             kubeClient,
		ExternalTinkerbell: true,
	}

	result, err := clusterController.Reconcile(context.TODO(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: clusterName, Namespace: clusterNamespace},
	})
	g.Expect(err).NotTo(HaveOccurred(), "Reconciling in external mode should succeed")
	g.Expect(result.RequeueAfter).To(BeNumerically(">", 0),
		"Expected a resync to be scheduled when Hardware cannot be watched")
}

func Test_Cluster_failure_domains_are_not_published_when_label_is_unset(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	domains, result := reconciledFailureDomains(t, []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, uuid.New().String(), hardwareIP, testOptions{
			Labels: map[string]string{rackLabel: "rack-a"},
		}),
	})

	g.Expect(domains).To(BeEmpty(), "Expected no failure domains when the label is not configured")
	g.Expect(result.IsZero()).To(BeTrue(), "Expected no resync when failure domains are not in use")
}

func Test_Cluster_failure_domains_ignore_hardware_without_a_label_value(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Hardware carrying the key with an empty value: label selectors match on key
	// presence, but "" is how Machine.spec.failureDomain spells "unassigned", so it
	// cannot name a domain anything could be placed into.
	empty := validHardware("hw-unlabelled", uuid.New().String(), "2.2.2.2", testOptions{
		Labels: map[string]string{rackLabel: ""},
	})

	labelled := validHardware("hw-labelled", uuid.New().String(), "3.3.3.3", testOptions{
		Labels: map[string]string{rackLabel: "rack-a"},
	})

	domains, _ := reconciledFailureDomains(t, []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		tinkerbellClusterWithFailureDomainLabel(),
		empty,
		labelled,
	})

	g.Expect(domains).To(HaveLen(1), "Expected the empty label value to be skipped")
	g.Expect(domains[0].Name).To(Equal("rack-a"), "Expected only the labelled rack to be published")
}

func Test_Cluster_failure_domains_are_empty_when_no_hardware_carries_the_label(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	domains, _ := reconciledFailureDomains(t, []runtime.Object{
		validCluster(clusterName, clusterNamespace),
		tinkerbellClusterWithFailureDomainLabel(),
		// No rack label at all.
		validHardware(hardwareName, uuid.New().String(), hardwareIP),
	})

	g.Expect(domains).To(BeEmpty(), "Expected no failure domains when no Hardware carries the label")
}
