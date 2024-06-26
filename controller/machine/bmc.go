package machine

import (
	"fmt"

	rufiov1 "github.com/tinkerbell/rufio/api/v1alpha1"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ensureHardwareProvisionJob ensures the hardware is ready to be provisioned.
// Uses the BMCRef from the hardware to create a BMCJob.
// The BMCJob is responsible for getting the machine to desired state for provisioning.
// If a BMCJob is already found and has failed, we error.
func (scope *machineReconcileScope) ensureHardwareProvisionJob(hw *tinkv1.Hardware) error {
	if hw.Spec.BMCRef == nil {
		scope.log.Info("Hardware BMC reference not present; skipping BMCJob creation",
			"BMCRef", hw.Spec.BMCRef, "Hardware", hw.Name)

		return nil
	}

	bmcJob := &rufiov1.Job{}
	jobName := fmt.Sprintf("%s-provision", scope.tinkerbellMachine.Name)

	err := scope.getBMCJob(jobName, bmcJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create a BMCJob for hardware provisioning
			return scope.createHardwareProvisionJob(hw, jobName)
		}

		return err
	}

	if bmcJob.HasCondition(rufiov1.JobFailed, rufiov1.ConditionTrue) {
		return fmt.Errorf("bmc job %s/%s failed", bmcJob.Namespace, bmcJob.Name) //nolint:goerr113
	}

	return nil
}

// getBMCJob fetches the BMCJob with name JName.
func (scope *machineReconcileScope) getBMCJob(jName string, bmj *rufiov1.Job) error {
	namespacedName := types.NamespacedName{
		Name:      jName,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	if err := scope.client.Get(scope.ctx, namespacedName, bmj); err != nil {
		return fmt.Errorf("GET BMCJob: %w", err)
	}

	return nil
}

// createHardwareProvisionJob creates a BMCJob object with the required tasks for hardware provisioning.
func (scope *machineReconcileScope) createHardwareProvisionJob(hw *tinkv1.Hardware, name string) error {
	job := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scope.tinkerbellMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       scope.tinkerbellMachine.Name,
					UID:        scope.tinkerbellMachine.ObjectMeta.UID,
				},
			},
		},
		Spec: rufiov1.JobSpec{
			MachineRef: rufiov1.MachineRef{
				Name:      hw.Spec.BMCRef.Name,
				Namespace: scope.tinkerbellMachine.Namespace,
			},
			Tasks: []rufiov1.Action{
				{
					PowerAction: rufiov1.PowerHardOff.Ptr(),
				},
				{
					OneTimeBootDeviceAction: &rufiov1.OneTimeBootDeviceAction{
						Devices: []rufiov1.BootDevice{
							rufiov1.PXE,
						},
						EFIBoot: hw.Spec.Interfaces[0].DHCP.UEFI,
					},
				},
				{
					PowerAction: rufiov1.PowerOn.Ptr(),
				},
			},
		},
	}

	if err := scope.client.Create(scope.ctx, job); err != nil {
		return fmt.Errorf("creating job: %w", err)
	}

	scope.log.Info("Created BMCJob to get hardware ready for provisioning",
		"Name", job.Name,
		"Namespace", job.Namespace)

	return nil
}

// createPowerOffJob creates a BMCJob object with the required tasks for hardware power off.
func (scope *machineReconcileScope) createPowerOffJob(hw *tinkv1.Hardware) error {
	controller := true
	bmcJob := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-poweroff", scope.tinkerbellMachine.Name),
			Namespace: scope.tinkerbellMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       scope.tinkerbellMachine.Name,
					UID:        scope.tinkerbellMachine.ObjectMeta.UID,
					Controller: &controller,
				},
			},
		},
		Spec: rufiov1.JobSpec{
			MachineRef: rufiov1.MachineRef{
				Name:      hw.Spec.BMCRef.Name,
				Namespace: scope.tinkerbellMachine.Namespace,
			},
			Tasks: []rufiov1.Action{
				{
					PowerAction: rufiov1.PowerHardOff.Ptr(),
				},
			},
		},
	}

	if err := scope.client.Create(scope.ctx, bmcJob); err != nil {
		return fmt.Errorf("creating BMCJob: %w", err)
	}

	scope.log.Info("Created BMCJob to power off hardware",
		"Name", bmcJob.Name,
		"Namespace", bmcJob.Namespace)

	return nil
}

// getJob fetches the Job by name.
func (scope *machineReconcileScope) getJob(name string, job *rufiov1.Job) error {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	if err := scope.client.Get(scope.ctx, namespacedName, job); err != nil {
		return fmt.Errorf("GET BMCJob: %w", err)
	}

	return nil
}

// ensureBMCJobCompletionForDelete ensures the machine power off BMCJob is completed.
// Removes the machint finalizer to let machine delete.
func (scope *machineReconcileScope) ensureBMCJobCompletionForDelete(hardware *tinkv1.Hardware) error {
	// Fetch a poweroff BMCJob for the machine.
	// If Job not found, we remove dependencies and create job.
	bmcJob := &rufiov1.Job{}
	jobName := fmt.Sprintf("%s-poweroff", scope.tinkerbellMachine.Name)

	if err := scope.getJob(jobName, bmcJob); err != nil {
		if apierrors.IsNotFound(err) {
			return scope.createPowerOffJob(hardware)
		}

		return fmt.Errorf("get bmc job for machine: %w", err)
	}

	// Check the Job conditions to ensure the power off job is complete.
	if bmcJob.HasCondition(rufiov1.JobCompleted, rufiov1.ConditionTrue) {
		return scope.removeFinalizer()
	}

	if bmcJob.HasCondition(rufiov1.JobFailed, rufiov1.ConditionTrue) {
		return fmt.Errorf("bmc job %s/%s failed", bmcJob.Namespace, bmcJob.Name) //nolint:goerr113
	}

	return nil
}
