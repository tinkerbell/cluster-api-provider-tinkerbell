package machine

import (
	"fmt"

	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"
)

const (
	// TinkerbellMachineHardwareClaimedCondition is True once the TinkerbellMachine owns a Hardware.
	TinkerbellMachineHardwareClaimedCondition = "HardwareClaimed"

	// TinkerbellMachineHardwareClaimedReason surfaces that the TinkerbellMachine owns a Hardware.
	TinkerbellMachineHardwareClaimedReason = "HardwareClaimed"

	// TinkerbellMachineWaitingForHardwareReason surfaces that no Hardware matching the
	// TinkerbellMachine is free to claim.
	TinkerbellMachineWaitingForHardwareReason = "WaitingForHardware"

	// TinkerbellMachineReadyReason surfaces that the TinkerbellMachine is provisioned.
	TinkerbellMachineReadyReason = clusterv1.ReadyReason

	// TinkerbellMachineNotProvisionedReason surfaces that provisioning has not finished.
	TinkerbellMachineNotProvisionedReason = "NotProvisioned"
)

func (scope *machineReconcileScope) setWaitingForHardwareCondition(err error) {
	conditions.Set(scope.tinkerbellMachine, metav1.Condition{
		Type:    TinkerbellMachineHardwareClaimedCondition,
		Status:  metav1.ConditionFalse,
		Reason:  TinkerbellMachineWaitingForHardwareReason,
		Message: "Waiting for Hardware: " + err.Error(),
	})
}

func (scope *machineReconcileScope) setHardwareClaimedCondition(hw *tinkv1.Hardware) {
	conditions.Set(scope.tinkerbellMachine, metav1.Condition{
		Type:    TinkerbellMachineHardwareClaimedCondition,
		Status:  metav1.ConditionTrue,
		Reason:  TinkerbellMachineHardwareClaimedReason,
		Message: fmt.Sprintf("Hardware %s/%s", hw.Namespace, hw.Name),
	})
}

// setReadyCondition summarizes the TinkerbellMachine state in the Ready condition.
// Cluster API copies this condition into the Machine's InfrastructureReady condition.
//
// Once the TinkerbellMachine is provisioned, Ready stays True. A Ready=False on a
// running Machine would mark it unavailable and could stall rollouts.
func (scope *machineReconcileScope) setReadyCondition() {
	m := scope.tinkerbellMachine

	if m.Status.Initialization != nil && ptr.Deref(m.Status.Initialization.Provisioned, false) {
		conditions.Set(m, metav1.Condition{
			Type:   clusterv1.ReadyCondition,
			Status: metav1.ConditionTrue,
			Reason: TinkerbellMachineReadyReason,
		})

		return
	}

	if c := conditions.Get(m, TinkerbellMachineHardwareClaimedCondition); c != nil && c.Status == metav1.ConditionFalse {
		conditions.Set(m, metav1.Condition{
			Type:    clusterv1.ReadyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  c.Reason,
			Message: c.Message,
		})

		return
	}

	conditions.Set(m, metav1.Condition{
		Type:    clusterv1.ReadyCondition,
		Status:  metav1.ConditionFalse,
		Reason:  TinkerbellMachineNotProvisionedReason,
		Message: "Hardware is not provisioned yet",
	})
}
