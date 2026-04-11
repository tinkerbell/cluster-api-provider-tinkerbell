package machine

import (
	"fmt"

	rufiov1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func toPtr[T any](v T) *T {
	return &v
}

// createPowerOffJob creates a BMCJob object with the required tasks for hardware power off.
func (scope *machineReconcileScope) createPowerOffJob(hw *tinkv1.Hardware) error {
	bmcJob := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-poweroff", scope.tinkerbellMachine.Name),
			Namespace: scope.tinkerbellNamespace(),
		},
		Spec: rufiov1.JobSpec{
			MachineRef: rufiov1.MachineRef{
				Name:      hw.Spec.BMCRef.Name,
				Namespace: scope.tinkerbellNamespace(),
			},
			Tasks: []rufiov1.Action{
				{
					PowerAction: toPtr(rufiov1.PowerHardOff),
				},
			},
		},
	}

	scope.setResourceOwnership(bmcJob)

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
	jobName := fmt.Sprintf("%s-poweroff", scope.tinkerbellMachine.Name)

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
