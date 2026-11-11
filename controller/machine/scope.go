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

package machine

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta2"
	tinkcluster "github.com/tinkerbell/cluster-api-provider-tinkerbell/pkg/cluster"
)

const (
	providerIDPlaceholder = "PROVIDER_ID"
)

var (
	// ErrMachineVersionEmpty is the error returned when Version is not set on the parent Machine.
	ErrMachineVersionEmpty = fmt.Errorf("machine version is empty")

	// ErrConfigurationNil is the error returned when TinkerbellMachineReconciler or TinkerbellClusterReconciler is nil.
	ErrConfigurationNil = fmt.Errorf("configuration is nil")

	// ErrMissingClient is the error returned when TinkerbellMachineReconciler or TinkerbellClusterReconciler do
	// not have a Client configured.
	ErrMissingClient = fmt.Errorf("client is nil")

	// ErrMissingTinkerbellClient is the error returned when TinkerbellMachineReconciler does not have
	// the required Tinkerbell client configured.
	ErrMissingTinkerbellClient = fmt.Errorf("tinkerbell client is nil")

	// ErrMissingScheme is the error returned when TinkerbellMachineReconciler does not have
	// a Scheme configured.
	ErrMissingScheme = fmt.Errorf("scheme is nil")

	// ErrMissingBootstrapDataSecretValueKey is the error returned when the Secret referenced for bootstrap data
	// is missing the value key.
	ErrMissingBootstrapDataSecretValueKey = fmt.Errorf("retrieving bootstrap data: secret value key is missing")

	// ErrBootstrapUserDataEmpty is the error returned when the referenced bootstrap data is empty.
	ErrBootstrapUserDataEmpty = fmt.Errorf("received bootstrap user data is empty")
)

type machineReconcileScope struct {
	log                  logr.Logger
	ctx                  context.Context
	tinkerbellMachine    *infrastructurev1.TinkerbellMachine
	patchHelper          *patch.Helper
	client               client.Client // management cluster client for CAPI objects
	scheme               *runtime.Scheme
	machine              *clusterv1.Machine
	tinkerbellCluster    *infrastructurev1.TinkerbellCluster
	bootstrapCloudConfig string

	// Tinkerbell cluster fields — in local mode tinkerbellClient == client;
	// in external mode it targets a separate Tinkerbell cluster.
	tinkerbellClient   client.Client
	externalTinkerbell bool // true when tinkerbellClient targets an external cluster
	watchManager       *tinkcluster.NamespaceWatchManager
}

func (scope *machineReconcileScope) addFinalizer() error {
	// Migrate from the legacy finalizer name (without a path separator) to the
	// new domain-qualified name. Remove the old one first to avoid carrying both.
	controllerutil.RemoveFinalizer(scope.tinkerbellMachine, infrastructurev1.MachineLegacyFinalizer)
	controllerutil.AddFinalizer(scope.tinkerbellMachine, infrastructurev1.MachineFinalizer)

	if err := scope.patch(); err != nil {
		return fmt.Errorf("patching TinkerbellMachine object with finalizer: %w", err)
	}

	return nil
}

// isExternal returns true when the Tinkerbell client targets an external
// Tinkerbell cluster, i.e. a different cluster than the CAPT management client.
func (scope *machineReconcileScope) isExternal() bool {
	return scope.externalTinkerbell
}

// tinkerbellNamespace returns the namespace on the Tinkerbell cluster where
// resources (Template, Workflow, Job) should be created or looked up.
// The value is persisted in Status.TargetNamespace during ensureHardware and
// must be set before this method is called.
func (scope *machineReconcileScope) tinkerbellNamespace() string {
	return scope.tinkerbellMachine.Status.TargetNamespace
}

// setResourceOwnership configures ownership on obj.
//
// Labels (LabelMachineName + LabelMachineNamespace) are always set so that
// label-based watches and cleanup work regardless of namespace placement.
//
// A standard controller owner reference is additionally set when the resource
// lives in the same namespace as the TinkerbellMachine AND we are not in
// external mode (owner references cannot cross namespace or cluster boundaries).
func (scope *machineReconcileScope) setResourceOwnership(obj client.Object) error {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string, 2)
	}

	labels[LabelMachineName] = scope.tinkerbellMachine.Name
	labels[LabelMachineNamespace] = scope.tinkerbellMachine.Namespace
	obj.SetLabels(labels)

	// Owner references only work within the same namespace on the same cluster.
	if !scope.isExternal() && obj.GetNamespace() == scope.tinkerbellMachine.Namespace {
		if err := controllerutil.SetControllerReference(scope.tinkerbellMachine, obj, scope.scheme); err != nil {
			return fmt.Errorf("setting controller reference: %w", err)
		}
	}

	return nil
}

type errRequeueRequested struct{}

func (e *errRequeueRequested) Error() string {
	return "requeue requested"
}

func (scope *machineReconcileScope) ensureTemplateAndWorkflow(hw *tinkv1.Hardware) (*tinkv1.Workflow, error) {
	wf, err := scope.getWorkflow()

	switch {
	case apierrors.IsNotFound(err):
		if err := scope.ensureTemplate(hw); err != nil {
			return nil, fmt.Errorf("failed to ensure template: %w", err)
		}

		if err := scope.createWorkflow(hw); err != nil {
			return nil, fmt.Errorf("failed to create workflow: %w", err)
		}

		return nil, &errRequeueRequested{}
	case err != nil:
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	default:
	}

	return wf, nil
}

func (scope *machineReconcileScope) Reconcile() error {
	defer func() {
		// make sure we do not create orphaned objects.
		if err := scope.addFinalizer(); err != nil {
			scope.log.Error(err, "error adding finalizer")
		}
	}()

	hw, err := scope.ensureHardware()
	if err != nil {
		return fmt.Errorf("failed to ensure hardware: %w", err)
	}

	return scope.reconcile(hw)
}

func (scope *machineReconcileScope) reconcile(hw *tinkv1.Hardware) error { //nolint:cyclop // this is broken up as best as possible, at the moment.
	// If the workflow has completed the TinkerbellMachine is ready.
	if v, found := hw.GetAnnotations()[HardwareProvisionedAnnotation]; found && v == "true" {
		scope.log.Info("Marking TinkerbellMachine as Ready")
		scope.tinkerbellMachine.Status.Ready = true
		scope.tinkerbellMachine.Status.Initialization = &infrastructurev1.TinkerbellMachineInitializationStatus{
			Provisioned: ptr.To(true),
		}

		return nil
	}

	wf, err := scope.ensureTemplateAndWorkflow(hw)
	if err != nil {
		if errors.Is(err, &errRequeueRequested{}) {
			return nil
		}

		return fmt.Errorf("ensure template and workflow returned: %w", err)
	}

	// STATE_* is needed for Helm charts <= 0.6.2
	switch wf.Status.State {
	case tinkv1.WorkflowStateFailed, tinkv1.WorkflowState("STATE_FAILED"):
		return errWorkflowFailed
	case tinkv1.WorkflowStateTimeout, tinkv1.WorkflowState("STATE_TIMEOUT"):
		return errWorkflowTimeout
	case tinkv1.WorkflowStatePreparing, tinkv1.WorkflowStatePending, tinkv1.WorkflowStateRunning, tinkv1.WorkflowStatePost:
		return nil
	case tinkv1.WorkflowStateSuccess, tinkv1.WorkflowState("STATE_SUCCESS"):
		scope.log.Info("Marking TinkerbellMachine as Ready")
		scope.tinkerbellMachine.Status.Ready = true
		scope.tinkerbellMachine.Status.Initialization = &infrastructurev1.TinkerbellMachineInitializationStatus{
			Provisioned: ptr.To(true),
		}

		if err := scope.patchHardwareAnnotations(hw, map[string]string{HardwareProvisionedAnnotation: "true"}); err != nil {
			return fmt.Errorf("failed to patch hardware: %w", err)
		}

		return nil
	default:
		return nil
	}
}

func (scope *machineReconcileScope) setStatus(hw *tinkv1.Hardware) error {
	if hw == nil {
		hw = &tinkv1.Hardware{}

		namespacedName := types.NamespacedName{
			Name:      scope.tinkerbellMachine.Spec.HardwareName,
			Namespace: scope.tinkerbellNamespace(),
		}

		if err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, hw); err != nil {
			return fmt.Errorf("getting Hardware: %w", err)
		}
	}

	ip, err := hardwareIP(hw)
	if err != nil {
		return fmt.Errorf("extracting Hardware IP address: %w", err)
	}

	scope.tinkerbellMachine.Status.Addresses = []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalIP,
			Address: ip,
		},
	}

	return scope.patch()
}

// MachineScheduledForDeletion implements machineReconcileContext interface method
// using TinkerbellMachine deletion timestamp.
func (scope *machineReconcileScope) MachineScheduledForDeletion() bool {
	return !scope.tinkerbellMachine.DeletionTimestamp.IsZero()
}

// DeleteMachineWithDependencies removes template and workflow objects associated with given machine.
func (scope *machineReconcileScope) DeleteMachineWithDependencies() error { //nolint:cyclop
	scope.log.Info("Removing machine", "hardwareName", scope.tinkerbellMachine.Spec.HardwareName)

	// If TargetNamespace was never persisted (e.g. the machine was deleted
	// before hardware selection completed), fall back to the machine's own
	// namespace so that cleanup lookups have a valid namespace.
	if scope.tinkerbellMachine.Status.TargetNamespace == "" {
		scope.tinkerbellMachine.Status.TargetNamespace = scope.tinkerbellMachine.Namespace
	}

	// Fetch hw for the machine.
	hw := &tinkv1.Hardware{}

	err := scope.getHardwareForMachine(hw)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// If the Hardware is not found, we can't do any BMC operations. In this case we just remove all
	// the other dependencies and remove the finalizer from the TinkerbellMachine object so that it can be deleted.
	if apierrors.IsNotFound(err) {
		scope.log.Info("Hardware not found, only template, workflow and finalizer will be removed",
			"hardwareName", scope.tinkerbellMachine.Spec.HardwareName,
		)

		if err := scope.removeDependencies(); err != nil {
			return err
		}

		return scope.removeFinalizer()
	}

	if err := scope.removeDependencies(); err != nil {
		return err
	}

	// The hardware BMCRef is nil.
	// Remove finalizers and let machine object delete.
	if hw.Spec.BMCRef == nil {
		scope.log.Info("Hardware BMC reference not present; skipping hardware power off",
			"BMCRef", hw.Spec.BMCRef, "Hardware", hw.Name)

		if err := scope.releaseHardware(hw); err != nil {
			return fmt.Errorf("error releasing Hardware: %w", err)
		}

		return scope.removeFinalizer()
	}

	if err := scope.ensureBMCJobCompletionForDelete(hw); err != nil {
		return fmt.Errorf("error ensuring BMC job completion for delete: %w", err)
	}

	if err := scope.releaseHardware(hw); err != nil {
		return fmt.Errorf("error releasing Hardware: %w", err)
	}

	if err := scope.removeFinalizer(); err != nil {
		return fmt.Errorf("error removing finalizer: %w", err)
	}

	return nil
}

// removeDependencies removes the Template and Workflow linked to the Machine/Hardware.
func (scope *machineReconcileScope) removeDependencies() error {
	if err := scope.removeTemplate(); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("removing Template: %w", err)
	}

	if err := scope.removeWorkflow(); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("removing Workflow: %w", err)
	}

	return nil
}

func (scope *machineReconcileScope) removeFinalizer() error {
	controllerutil.RemoveFinalizer(scope.tinkerbellMachine, infrastructurev1.MachineFinalizer)
	controllerutil.RemoveFinalizer(scope.tinkerbellMachine, infrastructurev1.MachineLegacyFinalizer)

	scope.log.Info("Patching Machine object to remove finalizer")

	return scope.patch()
}

// patch commits all done changes to TinkerbellMachine object. If patching fails, error
// is returned.
func (scope *machineReconcileScope) patch() error {
	// TODO: Improve control on when to patch the object.
	if err := scope.patchHelper.Patch(scope.ctx, scope.tinkerbellMachine); err != nil {
		return fmt.Errorf("patching machine object: %w", err)
	}

	return nil
}

// isMachineReady validates that given Machine object is ready for further processing.
//
// If machine is not ready, string reason is returned.
//
// If machine is ready, empty string is returned.
func isMachineReady(machine *clusterv1.Machine) (string, error) {
	if machine == nil {
		return "Machine Controller has not yet set OwnerRef", nil
	}

	if machine.Spec.Bootstrap.DataSecretName == nil {
		return "retrieving bootstrap data: linked Machine's bootstrap.dataSecretName is not available yet", nil
	}

	// Spec says this field is optional, but @detiber says it's effectively required,
	// so treat it as so.
	if machine.Spec.Version == "" {
		return "", ErrMachineVersionEmpty
	}

	return "", nil
}

// getReadyBootstrapCloudConfig returns initialized bootstrap cloud config for a given machine.
//
// If bootstrap cloud config is not yet initialized, empty string is returned.
func (scope *machineReconcileScope) getReadyBootstrapCloudConfig(machine *clusterv1.Machine) (string, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: machine.Namespace, Name: *machine.Spec.Bootstrap.DataSecretName}

	if err := scope.client.Get(scope.ctx, key, secret); err != nil {
		return "", fmt.Errorf("retrieving bootstrap data secret: %w", err)
	}

	bootstrapUserData, ok := secret.Data["value"]
	if !ok {
		return "", ErrMissingBootstrapDataSecretValueKey
	}

	if len(bootstrapUserData) == 0 {
		return "", ErrBootstrapUserDataEmpty
	}

	return string(bootstrapUserData), nil
}

// getTinkerbellCluster returns associated TinkerbellCluster object for a given machine.
func (scope *machineReconcileScope) getReadyTinkerbellCluster(machine *clusterv1.Machine) (*infrastructurev1.TinkerbellCluster, error) {
	cluster, err := util.GetClusterFromMetadata(scope.ctx, scope.client, machine.ObjectMeta)
	if err != nil {
		return nil, fmt.Errorf("getting cluster from metadata: %w", err)
	}

	tinkerbellCluster := &infrastructurev1.TinkerbellCluster{}
	tinkerbellClusterNamespacedName := client.ObjectKey{
		Namespace: scope.tinkerbellMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}

	if err := scope.client.Get(scope.ctx, tinkerbellClusterNamespacedName, tinkerbellCluster); err != nil {
		return nil, fmt.Errorf("getting TinkerbellCluster object: %w", err)
	}

	// Use OR (not AND): the v1beta2 transitional contract requires "one of"
	// status.ready or status.initialization.provisioned. Pre-v0.7 clusters
	// upgraded in-place only have status.ready set.
	if !tinkerbellCluster.Status.Ready &&
		(tinkerbellCluster.Status.Initialization == nil || tinkerbellCluster.Status.Initialization.Provisioned == nil || !*tinkerbellCluster.Status.Initialization.Provisioned) {
		scope.log.Info("cluster not ready yet")

		return nil, nil
	}

	return tinkerbellCluster, nil
}
