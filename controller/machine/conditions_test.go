package machine_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controller/machine"
)

func getTinkerbellMachine(t *testing.T, c client.Client) *infrastructurev1.TinkerbellMachine {
	t.Helper()

	tm := &infrastructurev1.TinkerbellMachine{}
	key := types.NamespacedName{Name: tinkerbellMachineName, Namespace: clusterNamespace}
	NewWithT(t).Expect(c.Get(context.Background(), key, tm)).To(Succeed())

	return tm
}

func Test_Machine_conditions_while_waiting_for_hardware_then_claiming_it(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardwareUUID := uuid.NewString()
	c := kubernetesClientWithObjects(t, []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	})

	_, err := reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	tm := getTinkerbellMachine(t, c)

	claimed := findCondition(tm.GetConditions(), machine.TinkerbellMachineHardwareClaimedCondition)
	g.Expect(claimed).NotTo(BeNil())
	g.Expect(claimed.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(claimed.Reason).To(Equal(machine.TinkerbellMachineWaitingForHardwareReason))
	g.Expect(claimed.Message).To(Equal("Waiting for Hardware: " + machine.ErrNoHardwareAvailable.Error()))

	ready := findCondition(tm.GetConditions(), clusterv1.ReadyCondition)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(claimed.Reason), "Ready should surface why the machine waits")
	g.Expect(ready.Message).To(Equal(claimed.Message))

	// The controller requeues while it waits. Those reconciles must not write.
	resourceVersion := tm.ResourceVersion
	_, err = reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(getTinkerbellMachine(t, c).ResourceVersion).To(Equal(resourceVersion))

	g.Expect(c.Create(context.Background(), validHardware(hardwareName, hardwareUUID, hardwareIP))).To(Succeed())

	_, err = reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	tm = getTinkerbellMachine(t, c)

	claimed = findCondition(tm.GetConditions(), machine.TinkerbellMachineHardwareClaimedCondition)
	g.Expect(claimed.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(claimed.Reason).To(Equal(machine.TinkerbellMachineHardwareClaimedReason))
	g.Expect(claimed.Message).To(Equal("Hardware " + clusterNamespace + "/" + hardwareName))

	ready = findCondition(tm.GetConditions(), clusterv1.ReadyCondition)
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(machine.TinkerbellMachineNotProvisionedReason))

	// The first reconcile after the claim creates the Workflow. Later ones must not write.
	_, err = reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	resourceVersion = getTinkerbellMachine(t, c).ResourceVersion
	_, err = reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(getTinkerbellMachine(t, c).ResourceVersion).To(Equal(resourceVersion))
}

func Test_Machine_conditions_ready_when_workflow_succeeds(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardwareUUID := uuid.NewString()
	c := kubernetesClientWithObjects(t, []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
		validTemplate(tinkerbellMachineName, clusterNamespace),
		validWorkflow(tinkerbellMachineName, clusterNamespace),
	})

	_, err := reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	tm := getTinkerbellMachine(t, c)

	ready := findCondition(tm.GetConditions(), clusterv1.ReadyCondition)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(ready.Reason).To(Equal(machine.TinkerbellMachineReadyReason))
	g.Expect(findCondition(tm.GetConditions(), machine.TinkerbellMachineHardwareClaimedCondition).Status).
		To(Equal(metav1.ConditionTrue))
}

func Test_Machine_conditions_not_ready_when_workflow_fails(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hardwareUUID := uuid.NewString()
	workflow := validWorkflow(tinkerbellMachineName, clusterNamespace)
	workflow.Status.State = tinkv1.WorkflowStateFailed

	c := kubernetesClientWithObjects(t, []runtime.Object{
		validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, hardwareUUID),
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validHardware(hardwareName, hardwareUUID, hardwareIP),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
		validTemplate(tinkerbellMachineName, clusterNamespace),
		workflow,
	})

	_, err := reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).To(HaveOccurred())

	ready := findCondition(getTinkerbellMachine(t, c).GetConditions(), clusterv1.ReadyCondition)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(machine.TinkerbellMachineNotProvisionedReason))
}

func Test_Machine_conditions_ready_stays_true_once_provisioned(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// A provisioned machine whose Hardware is gone: nothing matches, so the
	// claim reports WaitingForHardware, but Ready must stay True.
	tinkerbellMachine := validTinkerbellMachine(tinkerbellMachineName, clusterNamespace, machineName, uuid.NewString())
	tinkerbellMachine.Status.Initialization = &infrastructurev1.TinkerbellMachineInitializationStatus{
		Provisioned: ptr.To(true),
	}

	c := kubernetesClientWithObjects(t, []runtime.Object{
		tinkerbellMachine,
		validCluster(clusterName, clusterNamespace),
		validTinkerbellCluster(clusterName, clusterNamespace),
		validMachine(machineName, clusterNamespace, clusterName),
		validSecret(machineName, clusterNamespace),
	})

	_, err := reconcileMachineWithClient(c, tinkerbellMachineName, clusterNamespace)
	g.Expect(err).NotTo(HaveOccurred())

	tm := getTinkerbellMachine(t, c)

	g.Expect(findCondition(tm.GetConditions(), machine.TinkerbellMachineHardwareClaimedCondition).Status).
		To(Equal(metav1.ConditionFalse))

	ready := findCondition(tm.GetConditions(), clusterv1.ReadyCondition)
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
}
