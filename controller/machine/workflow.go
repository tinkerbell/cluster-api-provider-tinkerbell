package machine

import (
	"fmt"

	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// errWorkflowFailed is the error returned when the workflow fails.
var errWorkflowFailed = fmt.Errorf("workflow failed")

// lastActionStarted returns the state of the final action in a hardware's workflow or an error if the workflow
// has not reached the final action.
func lastActionStarted(wf *tinkv1.Workflow) bool {
	return wf.GetCurrentActionIndex() == wf.GetTotalNumberOfActions()-1
}

func (scope *machineReconcileScope) getWorkflow() (*tinkv1.Workflow, error) {
	namespacedName := types.NamespacedName{
		Name:      scope.tinkerbellMachine.Name,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	t := &tinkv1.Workflow{}

	err := scope.client.Get(scope.ctx, namespacedName, t)
	if err != nil {
		msg := "failed to get workflow: %w"
		if !apierrors.IsNotFound(err) {
			msg = "no workflow exists: %w"
		}

		return t, fmt.Errorf(msg, err) //nolint:goerr113
	}

	return t, nil
}

func (scope *machineReconcileScope) createWorkflow(hw *tinkv1.Hardware) error {
	c := true
	workflow := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scope.tinkerbellMachine.Name,
			Namespace: scope.tinkerbellMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       scope.tinkerbellMachine.Name,
					UID:        scope.tinkerbellMachine.ObjectMeta.UID,
					Controller: &c,
				},
			},
		},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: scope.tinkerbellMachine.Name,
			HardwareRef: hw.Name,
			HardwareMap: map[string]string{"device_1": hw.Spec.Metadata.Instance.ID},
		},
	}

	if err := scope.client.Create(scope.ctx, workflow); err != nil {
		return fmt.Errorf("creating workflow: %w", err)
	}

	return nil
}

// removeWorkflow makes sure workflow for TinkerbellMachine has been cleaned up.
func (scope *machineReconcileScope) removeWorkflow() error {
	namespacedName := types.NamespacedName{
		Name:      scope.tinkerbellMachine.Name,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	workflow := &tinkv1.Workflow{}

	err := scope.client.Get(scope.ctx, namespacedName, workflow)
	if err != nil {
		if apierrors.IsNotFound(err) {
			scope.log.Info("Workflow already removed", "name", namespacedName)

			return nil
		}

		return fmt.Errorf("checking if workflow exists: %w", err)
	}

	scope.log.Info("Removing Workflow", "name", namespacedName)

	if err := scope.client.Delete(scope.ctx, workflow); err != nil {
		return fmt.Errorf("ensuring workflow has been removed: %w", err)
	}

	return nil
}
