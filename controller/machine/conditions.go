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

	// TinkerbellMachineWaitingForHardwareReason surfaces that the TinkerbellMachine has
	// not claimed a Hardware yet.
	TinkerbellMachineWaitingForHardwareReason = "WaitingForHardware"

	// TinkerbellMachineHardwareNotFoundReason surfaces that a provisioned
	// TinkerbellMachine no longer owns any Hardware.
	TinkerbellMachineHardwareNotFoundReason = "HardwareNotFound"

	// TinkerbellMachineReadyReason surfaces that the TinkerbellMachine is provisioned.
	TinkerbellMachineReadyReason = clusterv1.ReadyReason

	// TinkerbellMachineNotProvisionedReason surfaces that provisioning has not finished.
	TinkerbellMachineNotProvisionedReason = "NotProvisioned"
)

// isProvisioned reports whether the TinkerbellMachine finished provisioning.
// Machines provisioned before v0.7.0 only carry Status.Ready, and keep it until a
// reconcile reaches the provisioned annotation and backfills Status.Initialization.
func (scope *machineReconcileScope) isProvisioned() bool {
	m := scope.tinkerbellMachine

	return m.Status.Ready || (m.Status.Initialization != nil && ptr.Deref(m.Status.Initialization.Provisioned, false))
}

func (scope *machineReconcileScope) setNoHardwareCondition(err error) {
	if scope.isProvisioned() {
		conditions.Set(scope.tinkerbellMachine, metav1.Condition{
			Type:    TinkerbellMachineHardwareClaimedCondition,
			Status:  metav1.ConditionFalse,
			Reason:  TinkerbellMachineHardwareNotFoundReason,
			Message: fmt.Sprintf("Provisioned machine no longer owns Hardware %q", scope.tinkerbellMachine.Spec.HardwareName),
		})

		return
	}

	scope.setWaitingForHardwareCondition("Waiting for Hardware: " + err.Error())
}

func (scope *machineReconcileScope) setWaitingForHardwareCondition(message string) {
	conditions.Set(scope.tinkerbellMachine, metav1.Condition{
		Type:    TinkerbellMachineHardwareClaimedCondition,
		Status:  metav1.ConditionFalse,
		Reason:  TinkerbellMachineWaitingForHardwareReason,
		Message: message,
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

	if scope.isProvisioned() {
		conditions.Set(m, metav1.Condition{
			Type:   clusterv1.ReadyCondition,
			Status: metav1.ConditionTrue,
			Reason: TinkerbellMachineReadyReason,
		})

		return
	}

	ready := metav1.Condition{Type: clusterv1.ReadyCondition, Status: metav1.ConditionFalse}

	switch claimed := conditions.Get(m, TinkerbellMachineHardwareClaimedCondition); {
	case claimed == nil:
		// ensureHardware failed before it could tell, for example on a Hardware API error.
		ready.Reason = TinkerbellMachineWaitingForHardwareReason
		ready.Message = "Waiting for Hardware"
	case claimed.Status != metav1.ConditionTrue:
		ready.Reason = claimed.Reason
		ready.Message = claimed.Message
	default:
		ready.Reason = TinkerbellMachineNotProvisionedReason
		ready.Message = "Hardware is not provisioned yet"
	}

	conditions.Set(m, ready)
}
