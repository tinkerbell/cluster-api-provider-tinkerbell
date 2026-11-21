package machine

import (
	"fmt"

	rufiov1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// powerOffJobName returns the name of the BMCJob that powers off the hardware when the
// TinkerbellMachine is deleted. The name is the TinkerbellMachine's UID, so a TinkerbellMachine
// recreated with the same name never mistakes the Job of an earlier delete, or the Tasks Rufio
// created for it, for its own. It leaves out the TinkerbellMachine's name because Rufio copies the
// Job name into a label value, which is limited to 63 characters; the Job's labels name the
// TinkerbellMachine instead.
func (scope *machineReconcileScope) powerOffJobName() string {
	return fmt.Sprintf("poweroff-%s", scope.tinkerbellMachine.UID)
}

// legacyPowerOffJobName returns the name earlier versions gave the power off BMCJob. They never
// deleted it when the Job had no owner reference, so it can still exist.
func (scope *machineReconcileScope) legacyPowerOffJobName() string {
	return fmt.Sprintf("%s-poweroff", scope.tinkerbellMachine.Name)
}

// createPowerOffJob creates a BMCJob object with the required tasks for hardware power off.
func (scope *machineReconcileScope) createPowerOffJob(hw *tinkv1.Hardware) error {
	bmcJob := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scope.powerOffJobName(),
			Namespace: scope.tinkerbellNamespace(),
		},
		Spec: rufiov1.JobSpec{
			MachineRef: rufiov1.MachineRef{
				Name:      hw.Spec.BMCRef.Name,
				Namespace: scope.tinkerbellNamespace(),
			},
			Tasks: []rufiov1.Action{
				{
					PowerAction: ptr.To(rufiov1.PowerHardOff),
				},
			},
		},
	}

	if err := scope.setResourceOwnership(bmcJob); err != nil {
		return fmt.Errorf("setting BMCJob ownership: %w", err)
	}

	if err := scope.tinkerbellClient.Create(scope.ctx, bmcJob); err != nil {
		return fmt.Errorf("creating BMCJob: %w", err)
	}

	scope.log.Info("Created BMCJob to power off hardware",
		"Name", bmcJob.Name,
		"Namespace", bmcJob.Namespace)

	return fmt.Errorf("requeue to wait for job.bmc completion: %s/%s", bmcJob.Namespace, bmcJob.Name)
}

// getJob fetches the Job by name.
func (scope *machineReconcileScope) getJob(name string, job *rufiov1.Job) error {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: scope.tinkerbellNamespace(),
	}

	if err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, job); err != nil {
		return fmt.Errorf("getting BMCJob: %w", err)
	}

	return nil
}

// ensureBMCJobCompletionForDelete ensures the machine power off BMCJob is completed.
// Removes the machint finalizer to let machine delete.
func (scope *machineReconcileScope) ensureBMCJobCompletionForDelete(hardware *tinkv1.Hardware) error {
	// Fetch a poweroff BMCJob for the machine.
	// If Job not found, we remove dependencies and create job.
	bmcJob := &rufiov1.Job{}
	jobName := scope.powerOffJobName()

	if err := scope.getJob(jobName, bmcJob); err != nil {
		if apierrors.IsNotFound(err) {
			return scope.createPowerOffJob(hardware)
		}

		return fmt.Errorf("getting BMC job for machine: %w", err)
	}

	// Check the Job conditions to ensure the power off job is complete.
	if bmcJob.HasCondition(rufiov1.JobCompleted, rufiov1.ConditionTrue) {
		return nil
	}

	if bmcJob.HasCondition(rufiov1.JobFailed, rufiov1.ConditionTrue) {
		return fmt.Errorf("bmc job %s/%s failed", bmcJob.Namespace, bmcJob.Name)
	}

	return fmt.Errorf("requeue, bmc job %s/%s is not completed", bmcJob.Namespace, bmcJob.Name)
}

// removePowerOffJobs deletes the power off BMCJob once the delete no longer needs it. A Job in
// another namespace or cluster has no owner reference, so nothing else garbage collects it. It
// also deletes a Job left by earlier versions under the legacy name, when its labels show it was
// created for this TinkerbellMachine.
func (scope *machineReconcileScope) removePowerOffJobs() error {
	if err := scope.removePowerOffJob(); err != nil {
		return err
	}

	return scope.removeLegacyPowerOffJob()
}

// removePowerOffJob deletes the power off BMCJob, if it exists. It reads the Job first, so a delete
// that never created one needs only get on Jobs, which Roles written before CAPT deleted Jobs
// already grant. It reads without the cache, which may not show a Job created moments ago.
func (scope *machineReconcileScope) removePowerOffJob() error {
	bmcJob := &rufiov1.Job{}
	key := types.NamespacedName{Name: scope.powerOffJobName(), Namespace: scope.tinkerbellNamespace()}

	if err := scope.tinkerbellReader.Get(scope.ctx, key, bmcJob); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("getting BMCJob %s: %w", key, err)
	}

	scope.log.Info("Removing BMCJob", "Name", bmcJob.Name, "Namespace", bmcJob.Namespace)

	if err := scope.tinkerbellClient.Delete(scope.ctx, bmcJob); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting BMCJob %s: %w", key, err)
	}

	return nil
}

// removeLegacyPowerOffJob deletes the power off BMCJob named the way earlier versions named it,
// if its labels show it was created for this TinkerbellMachine.
func (scope *machineReconcileScope) removeLegacyPowerOffJob() error {
	legacyJob := &rufiov1.Job{}
	if err := scope.getJob(scope.legacyPowerOffJobName(), legacyJob); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	labels := legacyJob.GetLabels()
	if labels[LabelMachineName] != scope.tinkerbellMachine.Name ||
		labels[LabelMachineNamespace] != scope.tinkerbellMachine.Namespace {
		return nil
	}

	scope.log.Info("Removing legacy BMCJob", "Name", legacyJob.Name, "Namespace", legacyJob.Namespace)

	if err := scope.tinkerbellClient.Delete(scope.ctx, legacyJob); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting legacy BMCJob %s/%s: %w", legacyJob.Namespace, legacyJob.Name, err)
	}

	return nil
}
