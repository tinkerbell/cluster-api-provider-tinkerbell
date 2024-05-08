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

package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	rufiov1 "github.com/tinkerbell/rufio/api/v1alpha1"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/internal/templates"
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

	// ErrMissingBootstrapDataSecretValueKey is the error returned when the Secret referenced for bootstrap data
	// is missing the value key.
	ErrMissingBootstrapDataSecretValueKey = fmt.Errorf("retrieving bootstrap data: secret value key is missing")

	// ErrBootstrapUserDataEmpty is the error returned when the referenced bootstrap data is empty.
	ErrBootstrapUserDataEmpty = fmt.Errorf("received bootstrap user data is empty")

	// ErrHardwareMissingDiskConfiguration is returned when the referenced hardware is missing
	// disk configuration.
	ErrHardwareMissingDiskConfiguration = fmt.Errorf("disk configuration is required")

	// errWorkflowFailed is the error returned when the workflow fails.
	errWorkflowFailed = fmt.Errorf("workflow failed")
)

type machineReconcileScope struct {
	log                  logr.Logger
	ctx                  context.Context
	tinkerbellMachine    *infrastructurev1.TinkerbellMachine
	patchHelper          *patch.Helper
	client               client.Client
	machine              *clusterv1.Machine
	tinkerbellCluster    *infrastructurev1.TinkerbellCluster
	bootstrapCloudConfig string
}

// lastActionStarted returns the state of the final action in a hardware's workflow or an error if the workflow
// has not reached the final action.
func lastActionStarted(wf *tinkv1.Workflow) bool {
	return wf.GetCurrentActionIndex() == wf.GetTotalNumberOfActions()-1
}

func (scope *machineReconcileScope) addFinalizer() error {
	controllerutil.AddFinalizer(scope.tinkerbellMachine, infrastructurev1.MachineFinalizer)

	if err := scope.patch(); err != nil {
		return fmt.Errorf("patching TinkerbellMachine object with finalizer: %w", err)
	}

	return nil
}

func isHardwareReady(hw *tinkv1.Hardware) bool {
	// if allowpxe false for all interface, hardware ready
	if len(hw.Spec.Interfaces) == 0 {
		return false
	}

	for _, ifc := range hw.Spec.Interfaces {
		if ifc.Netboot != nil {
			if *ifc.Netboot.AllowPXE {
				return false
			}
		}
	}

	return true
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

func (scope *machineReconcileScope) reconcile(hw *tinkv1.Hardware) error {
	if isHardwareReady(hw) {
		scope.log.Info("Marking TinkerbellMachine as Ready")
		scope.tinkerbellMachine.Status.Ready = true

		return nil
	}

	if ensureJobErr := scope.ensureHardwareProvisionJob(hw); ensureJobErr != nil {
		return fmt.Errorf("failed to ensure hardware ready for provisioning: %w", ensureJobErr)
	}

	wf, err := scope.ensureTemplateAndWorkflow(hw)

	switch {
	case errors.Is(err, &errRequeueRequested{}):
		return nil
	case err != nil:
		return fmt.Errorf("ensure template and workflow returned: %w", err)
	}

	s := wf.GetCurrentActionState()
	if s == tinkv1.WorkflowStateFailed || s == tinkv1.WorkflowStateTimeout {
		return errWorkflowFailed
	}

	if !lastActionStarted(wf) {
		return nil
	}

	if err := scope.patchHardwareStates(hw, false); err != nil {
		return fmt.Errorf("failed to patch hardware: %w", err)
	}

	scope.log.Info("Marking TinkerbellMachine as Ready")
	scope.tinkerbellMachine.Status.Ready = true

	return nil
}

// patchHardwareStates patches a hardware's metadata and instance states.
func (scope *machineReconcileScope) patchHardwareStates(hw *tinkv1.Hardware, allowpxe bool) error {
	patchHelper, err := patch.NewHelper(hw, scope.client)
	if err != nil {
		return fmt.Errorf("initializing patch helper for selected hardware: %w", err)
	}

	for _, ifc := range hw.Spec.Interfaces {
		if ifc.Netboot != nil {
			ifc.Netboot.AllowPXE = ptr.To(allowpxe)
		}
	}

	if err := patchHelper.Patch(scope.ctx, hw); err != nil {
		return fmt.Errorf("patching Hardware object: %w", err)
	}

	return nil
}

func (scope *machineReconcileScope) templateExists() (bool, error) {
	namespacedName := types.NamespacedName{
		Name:      scope.tinkerbellMachine.Name,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	err := scope.client.Get(scope.ctx, namespacedName, &tinkv1.Template{})
	if err == nil {
		return true, nil
	}

	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("checking if template exists: %w", err)
	}

	return false, nil
}

func (scope *machineReconcileScope) imageURL() (string, error) {
	imageLookupFormat := scope.tinkerbellMachine.Spec.ImageLookupFormat
	if imageLookupFormat == "" {
		imageLookupFormat = scope.tinkerbellCluster.Spec.ImageLookupFormat
	}

	imageLookupBaseRegistry := scope.tinkerbellMachine.Spec.ImageLookupBaseRegistry
	if imageLookupBaseRegistry == "" {
		imageLookupBaseRegistry = scope.tinkerbellCluster.Spec.ImageLookupBaseRegistry
	}

	imageLookupOSDistro := scope.tinkerbellMachine.Spec.ImageLookupOSDistro
	if imageLookupOSDistro == "" {
		imageLookupOSDistro = scope.tinkerbellCluster.Spec.ImageLookupOSDistro
	}

	imageLookupOSVersion := scope.tinkerbellMachine.Spec.ImageLookupOSVersion
	if imageLookupOSVersion == "" {
		imageLookupOSVersion = scope.tinkerbellCluster.Spec.ImageLookupOSVersion
	}

	return imageURL(
		imageLookupFormat,
		imageLookupBaseRegistry,
		imageLookupOSDistro,
		imageLookupOSVersion,
		*scope.machine.Spec.Version,
	)
}

func (scope *machineReconcileScope) createTemplate(hardware *tinkv1.Hardware) error {
	if len(hardware.Spec.Disks) < 1 {
		return ErrHardwareMissingDiskConfiguration
	}

	templateData := scope.tinkerbellMachine.Spec.TemplateOverride
	if templateData == "" {
		targetDisk := hardware.Spec.Disks[0].Device
		targetDevice := firstPartitionFromDevice(targetDisk)

		imageURL, err := scope.imageURL()
		if err != nil {
			return fmt.Errorf("failed to generate imageURL: %w", err)
		}

		metadataIP := os.Getenv("TINKERBELL_IP")
		if metadataIP == "" {
			metadataIP = "192.168.1.1"
		}

		metadataURL := fmt.Sprintf("http://%s:50061", metadataIP)

		workflowTemplate := templates.WorkflowTemplate{
			Name:          scope.tinkerbellMachine.Name,
			MetadataURL:   metadataURL,
			ImageURL:      imageURL,
			DestDisk:      targetDisk,
			DestPartition: targetDevice,
		}

		templateData, err = workflowTemplate.Render()
		if err != nil {
			return fmt.Errorf("rendering template: %w", err)
		}
	}

	templateObject := &tinkv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scope.tinkerbellMachine.Name,
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
		Spec: tinkv1.TemplateSpec{
			Data: &templateData,
		},
	}

	if err := scope.client.Create(scope.ctx, templateObject); err != nil {
		return fmt.Errorf("creating Tinkerbell template: %w", err)
	}

	return nil
}

func firstPartitionFromDevice(device string) string {
	nvmeDevice := regexp.MustCompile(`^/dev/nvme\d+n\d+$`)
	emmcDevice := regexp.MustCompile(`^/dev/mmcblk\d+$`)

	switch {
	case nvmeDevice.MatchString(device), emmcDevice.MatchString(device):
		return fmt.Sprintf("%sp1", device)
	default:
		return fmt.Sprintf("%s1", device)
	}
}

func (scope *machineReconcileScope) ensureTemplate(hardware *tinkv1.Hardware) error {
	// TODO: should this reconccile the template instead of just ensuring it exists?
	templateExists, err := scope.templateExists()
	if err != nil {
		return fmt.Errorf("checking if Template exists: %w", err)
	}

	if templateExists {
		return nil
	}

	scope.log.Info("template for machine does not exist, creating")

	return scope.createTemplate(hardware)
}

func (scope *machineReconcileScope) takeHardwareOwnership(hardware *tinkv1.Hardware) error {
	if len(hardware.ObjectMeta.Labels) == 0 {
		hardware.ObjectMeta.Labels = map[string]string{}
	}

	hardware.ObjectMeta.Labels[HardwareOwnerNameLabel] = scope.tinkerbellMachine.Name
	hardware.ObjectMeta.Labels[HardwareOwnerNamespaceLabel] = scope.tinkerbellMachine.Namespace

	// Add finalizer to hardware as well to make sure we release it before Machine object is removed.
	controllerutil.AddFinalizer(hardware, infrastructurev1.MachineFinalizer)

	if err := scope.client.Update(scope.ctx, hardware); err != nil {
		return fmt.Errorf("updating Hardware object: %w", err)
	}

	return nil
}

func (scope *machineReconcileScope) setStatus(hardware *tinkv1.Hardware) error {
	if hardware == nil {
		hardware = &tinkv1.Hardware{}

		namespacedName := types.NamespacedName{
			Name:      scope.tinkerbellMachine.Spec.HardwareName,
			Namespace: scope.tinkerbellMachine.Namespace,
		}

		if err := scope.client.Get(scope.ctx, namespacedName, hardware); err != nil {
			return fmt.Errorf("getting Hardware: %w", err)
		}
	}

	ip, err := hardwareIP(hardware)
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

func (scope *machineReconcileScope) ensureHardwareUserData(hardware *tinkv1.Hardware, providerID string) error {
	userData := strings.ReplaceAll(scope.bootstrapCloudConfig, providerIDPlaceholder, providerID)

	if hardware.Spec.UserData == nil || *hardware.Spec.UserData != userData {
		patchHelper, err := patch.NewHelper(hardware, scope.client)
		if err != nil {
			return fmt.Errorf("initializing patch helper for selected hardware: %w", err)
		}

		hardware.Spec.UserData = &userData

		if err := patchHelper.Patch(scope.ctx, hardware); err != nil {
			return fmt.Errorf("patching Hardware object: %w", err)
		}
	}

	return nil
}

func (scope *machineReconcileScope) ensureHardware() (*tinkv1.Hardware, error) {
	hardware, err := scope.hardwareForMachine()
	if err != nil {
		return nil, fmt.Errorf("getting hardware: %w", err)
	}

	if err := scope.takeHardwareOwnership(hardware); err != nil {
		return nil, fmt.Errorf("taking Hardware ownership: %w", err)
	}

	if scope.tinkerbellMachine.Spec.HardwareName == "" {
		scope.log.Info("Selected Hardware for machine", "Hardware name", hardware.Name)
	}

	scope.tinkerbellMachine.Spec.HardwareName = hardware.Name
	scope.tinkerbellMachine.Spec.ProviderID = fmt.Sprintf("tinkerbell://%s/%s", hardware.Namespace, hardware.Name)

	if err := scope.ensureHardwareUserData(hardware, scope.tinkerbellMachine.Spec.ProviderID); err != nil {
		return nil, fmt.Errorf("ensuring Hardware user data: %w", err)
	}

	return hardware, scope.setStatus(hardware)
}

func (scope *machineReconcileScope) hardwareForMachine() (*tinkv1.Hardware, error) {
	// first query for hardware that's already assigned
	if hardware, err := scope.assignedHardware(); err != nil {
		return nil, err
	} else if hardware != nil {
		return hardware, nil
	}

	// then fallback to searching for new hardware
	hardwareSelector := scope.tinkerbellMachine.Spec.HardwareAffinity.DeepCopy()
	if hardwareSelector == nil {
		hardwareSelector = &infrastructurev1.HardwareAffinity{}
	}
	// if no terms are specified, we create an empty one to ensure we always query for non-selected hardware
	if len(hardwareSelector.Required) == 0 {
		hardwareSelector.Required = append(hardwareSelector.Required, infrastructurev1.HardwareAffinityTerm{})
	}

	var matchingHardware []tinkv1.Hardware

	// OR all of the required terms by selecting each individually, we could end up with duplicates in matchingHardware
	// but it doesn't matter
	for i := range hardwareSelector.Required {
		var matched tinkv1.HardwareList

		// add a selector for unselected hardware
		hardwareSelector.Required[i].LabelSelector.MatchExpressions = append(
			hardwareSelector.Required[i].LabelSelector.MatchExpressions,
			metav1.LabelSelectorRequirement{
				Key:      HardwareOwnerNameLabel,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			})

		selector, err := metav1.LabelSelectorAsSelector(&hardwareSelector.Required[i].LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("converting label selector: %w", err)
		}

		if err := scope.client.List(scope.ctx, &matched, &client.ListOptions{LabelSelector: selector}); err != nil {
			return nil, fmt.Errorf("listing hardware without owner: %w", err)
		}

		matchingHardware = append(matchingHardware, matched.Items...)
	}

	// finally sort by our preferred affinity terms
	cmp, err := byHardwareAffinity(matchingHardware, hardwareSelector.Preferred)
	if err != nil {
		return nil, fmt.Errorf("sorting hardware by preference: %w", err)
	}

	sort.Slice(matchingHardware, cmp)

	if len(matchingHardware) > 0 {
		return &matchingHardware[0], nil
	}
	// nothing was found
	return nil, ErrNoHardwareAvailable
}

// assignedHardware returns hardware that is already assigned. In the event of no hardware being assigned, it returns
// nil, nil.
func (scope *machineReconcileScope) assignedHardware() (*tinkv1.Hardware, error) {
	var selectedHardware tinkv1.HardwareList
	if err := scope.client.List(scope.ctx, &selectedHardware, client.MatchingLabels{
		HardwareOwnerNameLabel:      scope.tinkerbellMachine.Name,
		HardwareOwnerNamespaceLabel: scope.tinkerbellMachine.Namespace,
	}); err != nil {
		return nil, fmt.Errorf("listing hardware with owner: %w", err)
	}

	if len(selectedHardware.Items) > 0 {
		return &selectedHardware.Items[0], nil
	}

	return nil, nil
}

//nolint:lll
func byHardwareAffinity(hardware []tinkv1.Hardware, preferred []infrastructurev1.WeightedHardwareAffinityTerm) (func(i int, j int) bool, error) {
	scores := map[client.ObjectKey]int32{}
	// compute scores for each item based on the preferred term weights
	for _, term := range preferred {
		term := term

		selector, err := metav1.LabelSelectorAsSelector(&term.HardwareAffinityTerm.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("constructing label selector: %w", err)
		}

		for i := range hardware {
			hw := &hardware[i]
			if selector.Matches(labels.Set(hw.Labels)) {
				scores[client.ObjectKeyFromObject(hw)] = term.Weight
			}
		}
	}

	return func(i, j int) bool {
		lhsScore := scores[client.ObjectKeyFromObject(&hardware[i])]
		rhsScore := scores[client.ObjectKeyFromObject(&hardware[j])]
		// sort by score in descending order
		if lhsScore > rhsScore {
			return true
		} else if lhsScore < rhsScore {
			return false
		}

		// just give a consistent ordering so we predictably pick one if scores are equal
		if hardware[i].Namespace != hardware[j].Namespace {
			return hardware[i].Namespace < hardware[j].Namespace
		}

		return hardware[i].Name < hardware[j].Name
	}, nil
}

// ensureHardwareProvisionJob ensures the hardware is ready to be provisioned.
// Uses the BMCRef from the hardware to create a BMCJob.
// The BMCJob is responsible for getting the machine to desired state for provisioning.
// If a BMCJob is already found and has failed, we error.
func (scope *machineReconcileScope) ensureHardwareProvisionJob(hardware *tinkv1.Hardware) error {
	if hardware.Spec.BMCRef == nil {
		scope.log.Info("Hardware BMC reference not present; skipping BMCJob creation",
			"BMCRef", hardware.Spec.BMCRef, "Hardware", hardware.Name)

		return nil
	}

	bmcJob := &rufiov1.Job{}
	jobName := fmt.Sprintf("%s-provision", scope.tinkerbellMachine.Name)

	err := scope.getBMCJob(jobName, bmcJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create a BMCJob for hardware provisioning
			return scope.createHardwareProvisionJob(hardware, jobName)
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
func (scope *machineReconcileScope) createHardwareProvisionJob(hardware *tinkv1.Hardware, name string) error {
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
				Name:      hardware.Spec.BMCRef.Name,
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
						EFIBoot: hardware.Spec.Interfaces[0].DHCP.UEFI,
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

func (scope *machineReconcileScope) createWorkflow(hardware *tinkv1.Hardware) error {
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
			HardwareRef: hardware.Name,
			HardwareMap: map[string]string{"device_1": hardware.Spec.Metadata.Instance.ID},
		},
	}

	if err := scope.client.Create(scope.ctx, workflow); err != nil {
		return fmt.Errorf("creating workflow: %w", err)
	}

	return nil
}

// MachineScheduledForDeletion implements machineReconcileContext interface method
// using TinkerbellMachine deletion timestamp.
func (scope *machineReconcileScope) MachineScheduledForDeletion() bool {
	return !scope.tinkerbellMachine.ObjectMeta.DeletionTimestamp.IsZero()
}

func (scope *machineReconcileScope) releaseHardware(hardware *tinkv1.Hardware) error {
	patchHelper, err := patch.NewHelper(hardware, scope.client)
	if err != nil {
		return fmt.Errorf("initializing patch helper for selected hardware: %w", err)
	}

	delete(hardware.ObjectMeta.Labels, HardwareOwnerNameLabel)
	delete(hardware.ObjectMeta.Labels, HardwareOwnerNamespaceLabel)
	// setting the AllowPXE=true indicates to Smee that this hardware should be allowed
	// to netboot. FYI, this is not authoritative.
	// Other hardware values can be set to prohibit netbooting of a machine.
	// See this Boots function for the logic around this: https://github.com/tinkerbell/smee/blob/main/internal/ipxe/script/ipxe.go#L112
	for _, ifc := range hardware.Spec.Interfaces {
		if ifc.Netboot != nil {
			ifc.Netboot.AllowPXE = ptr.To(true)
		}
	}

	controllerutil.RemoveFinalizer(hardware, infrastructurev1.MachineFinalizer)

	if err := patchHelper.Patch(scope.ctx, hardware); err != nil {
		return fmt.Errorf("patching Hardware object: %w", err)
	}

	return nil
}

func (scope *machineReconcileScope) getHardwareForMachine(hardware *tinkv1.Hardware) error {
	namespacedName := types.NamespacedName{
		Name:      scope.tinkerbellMachine.Spec.HardwareName,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	if err := scope.client.Get(scope.ctx, namespacedName, hardware); err != nil {
		return fmt.Errorf("getting hardware: %w", err)
	}

	return nil
}

// createPowerOffJob creates a BMCJob object with the required tasks for hardware power off.
func (scope *machineReconcileScope) createPowerOffJob(hardware *tinkv1.Hardware) error {
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
				Name:      hardware.Spec.BMCRef.Name,
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

// DeleteMachineWithDependencies removes template and workflow objects associated with given machine.
func (scope *machineReconcileScope) DeleteMachineWithDependencies() error {
	scope.log.Info("Removing machine", "hardwareName", scope.tinkerbellMachine.Spec.HardwareName)
	// Fetch hardware for the machine.
	hardware := &tinkv1.Hardware{}
	if err := scope.getHardwareForMachine(hardware); err != nil {
		return err
	}

	if err := scope.removeDependencies(hardware); err != nil {
		return err
	}

	// The hardware BMCRef is nil.
	// Remove finalizers and let machine object delete.
	if hardware.Spec.BMCRef == nil {
		scope.log.Info("Hardware BMC reference not present; skipping hardware power off",
			"BMCRef", hardware.Spec.BMCRef, "Hardware", hardware.Name)

		return scope.removeFinalizer()
	}

	return scope.ensureBMCJobCompletionForDelete(hardware)
}

// removeDependencies removes the Template, Workflow linked to the machine.
// Deletes the machine hardware labels for the machine.
func (scope *machineReconcileScope) removeDependencies(hardware *tinkv1.Hardware) error {
	if err := scope.removeTemplate(); err != nil {
		return fmt.Errorf("removing Template: %w", err)
	}

	if err := scope.removeWorkflow(); err != nil {
		return fmt.Errorf("removing Workflow: %w", err)
	}

	if err := scope.releaseHardware(hardware); err != nil {
		return fmt.Errorf("releasing Hardware: %w", err)
	}

	return nil
}

func (scope *machineReconcileScope) removeFinalizer() error {
	controllerutil.RemoveFinalizer(scope.tinkerbellMachine, infrastructurev1.MachineFinalizer)

	scope.log.Info("Patching Machine object to remove finalizer")

	return scope.patch()
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

// removeTemplate makes sure template for TinkerbellMachine has been cleaned up.
func (scope *machineReconcileScope) removeTemplate() error {
	namespacedName := types.NamespacedName{
		Name:      scope.tinkerbellMachine.Name,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	template := &tinkv1.Template{}

	err := scope.client.Get(scope.ctx, namespacedName, template)
	if err != nil {
		if apierrors.IsNotFound(err) {
			scope.log.Info("Template already removed", "name", namespacedName)

			return nil
		}

		return fmt.Errorf("checking if template exists: %w", err)
	}

	scope.log.Info("Removing Template", "name", namespacedName)

	if err := scope.client.Delete(scope.ctx, template); err != nil {
		return fmt.Errorf("ensuring template has been removed: %w", err)
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

// patch commits all done changes to TinkerbellMachine object. If patching fails, error
// is returned.
func (scope *machineReconcileScope) patch() error {
	// TODO: Improve control on when to patch the object.
	if err := scope.patchHelper.Patch(scope.ctx, scope.tinkerbellMachine); err != nil {
		return fmt.Errorf("patching machine object: %w", err)
	}

	return nil
}

// getReadyMachine returns valid ClusterAPI Machine object.
//
// If error occurs while fetching the machine, error is returned.
//
// If machine is not ready yet, nil is returned.
func (scope *machineReconcileScope) getReadyMachine() (*clusterv1.Machine, error) {
	// Continue building the context with some validation rules.
	machine, err := util.GetOwnerMachine(scope.ctx, scope.client, scope.tinkerbellMachine.ObjectMeta)
	if err != nil {
		return nil, fmt.Errorf("getting Machine object: %w", err)
	}

	reason, err := isMachineReady(machine)
	if err != nil {
		return nil, fmt.Errorf("validating Machine object: %w", err)
	}

	if reason != "" {
		scope.log.Info("machine is not ready yet", "reason", reason)

		return nil, nil
	}

	return machine, nil
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
	if machine.Spec.Version == nil || *machine.Spec.Version == "" {
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
func (scope *machineReconcileScope) getReadyTinkerbellCluster(machine *clusterv1.Machine) (*infrastructurev1.TinkerbellCluster, error) { //nolint:lll
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

	if !tinkerbellCluster.Status.Ready {
		scope.log.Info("cluster not ready yet")

		return nil, nil
	}

	return tinkerbellCluster, nil
}

type image struct {
	BaseRegistry      string
	OSDistro          string
	OSVersion         string
	KubernetesVersion string
}

func imageURL(imageFormat, baseRegistry, osDistro, osVersion, kubernetesVersion string) (string, error) {
	imageParams := image{
		BaseRegistry:      baseRegistry,
		OSDistro:          strings.ToLower(osDistro),
		OSVersion:         strings.ReplaceAll(osVersion, ".", ""),
		KubernetesVersion: kubernetesVersion,
	}

	var buf bytes.Buffer

	template, err := template.New("image").Parse(imageFormat)
	if err != nil {
		return "", fmt.Errorf("failed to create template from string %q: %w", imageFormat, err)
	}

	if err := template.Execute(&buf, imageParams); err != nil {
		return "", fmt.Errorf("failed to populate template %q: %w", imageFormat, err)
	}

	return buf.String(), nil
}
