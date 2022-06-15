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
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rufiov1 "github.com/tinkerbell/rufio/api/v1alpha1"
	tinkv1 "github.com/tinkerbell/tink/pkg/apis/core/v1alpha1"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/internal/templates"
)

const (
	providerIDPlaceholder = "PROVIDER_ID"
	inUse                 = "in_use"
	provisioned           = "provisioned"
)

type machineReconcileContext struct {
	*baseMachineReconcileContext

	machine              *clusterv1.Machine
	tinkerbellCluster    *infrastructurev1.TinkerbellCluster
	bootstrapCloudConfig string
}

// ErrHardwareMissingDiskConfiguration is returned when the referenced hardware is missing
// disk configuration.
var ErrHardwareMissingDiskConfiguration = fmt.Errorf("disk configuration is required")

// MachineCreator is a subset of tinkerbellCluster used by machineReconcileContext.
type MachineCreator interface {
	// Template related functions.
	CreateTemplate(ctx context.Context, name, data string) (string, error)

	// Workflow related functions.
	CreateWorkflow(ctx context.Context, templateID, hardware string) (string, error)

	// Hardware related functions.
	HardwareIDByIP(ctx context.Context, ip string) (string, error)
	GetHardwareIP(ctx context.Context, id string) (string, error)
	NextAvailableHardwareID(ctx context.Context) (string, error)
	HardwareAvailable(ctx context.Context, id string) (bool, error)
}

// lastActionStarted returns the state of the final action in a hardware's workflow or an error if the workflow
// has not reached the final action.
func lastActionStarted(wf *tinkv1.Workflow) bool {
	return wf.GetCurrentActionIndex() == wf.GetTotalNumberOfActions()-1
}

func (mrc *machineReconcileContext) addFinalizer() error {
	controllerutil.AddFinalizer(mrc.tinkerbellMachine, infrastructurev1.MachineFinalizer)

	if err := mrc.patch(); err != nil {
		return fmt.Errorf("patching TinkerbellMachine object with finalizer: %w", err)
	}

	return nil
}

func isHardwareReady(hw *tinkv1.Hardware) bool {
	return hw.Spec.Metadata.State == inUse && hw.Spec.Metadata.Instance.State == provisioned
}

type errRequeueRequested struct{}

func (e *errRequeueRequested) Error() string {
	return "requeue requested"
}

func (mrc *machineReconcileContext) ensureTemplateAndWorkflow(hw *tinkv1.Hardware) (*tinkv1.Workflow, error) {
	wf, err := mrc.getWorkflow()

	switch {
	case apierrors.IsNotFound(err):
		if err := mrc.ensureTemplate(hw); err != nil {
			return nil, fmt.Errorf("failed to ensure template: %w", err)
		}

		if err := mrc.createWorkflow(hw); err != nil {
			return nil, fmt.Errorf("failed to create workflow: %w", err)
		}

		return nil, &errRequeueRequested{}
	case err != nil:
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	default:
	}

	return wf, nil
}

//
func (mrc *machineReconcileContext) Reconcile() error {
	defer func() {
		// make sure we do not create orphaned objects.
		if err := mrc.addFinalizer(); err != nil {
			mrc.log.Error(err, "error adding finalizer")
		}
	}()

	hw, err := mrc.ensureHardware()
	if err != nil {
		return fmt.Errorf("failed to ensure hardware: %w", err)
	}

	return mrc.reconcile(hw)
}

func (mrc *machineReconcileContext) reconcile(hw *tinkv1.Hardware) error {
	if !isHardwareReady(hw) {
		wf, err := mrc.ensureTemplateAndWorkflow(hw)

		if ensureJobErr := mrc.ensureHardwareProvisionJob(hw); ensureJobErr != nil {
			return fmt.Errorf("failed to ensure hardware ready for provisioning: %w", ensureJobErr)
		}

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

		if err := mrc.patchHardwareStates(hw, inUse, provisioned); err != nil {
			return fmt.Errorf("failed to patch hardware: %w", err)
		}
	}

	mrc.log.Info("Marking TinkerbellMachine as Ready")

	mrc.tinkerbellMachine.Status.Ready = true

	return nil
}

// patchHardwareStates patches a hardware's metadata and instance states.
func (mrc *machineReconcileContext) patchHardwareStates(hw *tinkv1.Hardware, mdState, iState string) error {
	patchHelper, err := patch.NewHelper(hw, mrc.client)
	if err != nil {
		return fmt.Errorf("initializing patch helper for selected hardware: %w", err)
	}

	hw.Spec.Metadata.State = mdState
	hw.Spec.Metadata.Instance.State = iState

	if err := patchHelper.Patch(mrc.ctx, hw); err != nil {
		return fmt.Errorf("patching Hardware object: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) templateExists() (bool, error) {
	namespacedName := types.NamespacedName{
		Name:      mrc.tinkerbellMachine.Name,
		Namespace: mrc.tinkerbellMachine.Namespace,
	}

	err := mrc.client.Get(mrc.ctx, namespacedName, &tinkv1.Template{})
	if err == nil {
		return true, nil
	}

	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("checking if template exists: %w", err)
	}

	return false, nil
}

func (mrc *machineReconcileContext) imageURL() (string, error) {
	imageLookupFormat := mrc.tinkerbellMachine.Spec.ImageLookupFormat
	if imageLookupFormat == "" {
		imageLookupFormat = mrc.tinkerbellCluster.Spec.ImageLookupFormat
	}

	imageLookupBaseRegistry := mrc.tinkerbellMachine.Spec.ImageLookupBaseRegistry
	if imageLookupBaseRegistry == "" {
		imageLookupBaseRegistry = mrc.tinkerbellCluster.Spec.ImageLookupBaseRegistry
	}

	imageLookupOSDistro := mrc.tinkerbellMachine.Spec.ImageLookupOSDistro
	if imageLookupOSDistro == "" {
		imageLookupOSDistro = mrc.tinkerbellCluster.Spec.ImageLookupOSDistro
	}

	imageLookupOSVersion := mrc.tinkerbellMachine.Spec.ImageLookupOSVersion
	if imageLookupOSVersion == "" {
		imageLookupOSVersion = mrc.tinkerbellCluster.Spec.ImageLookupOSVersion
	}

	return imageURL(
		imageLookupFormat,
		imageLookupBaseRegistry,
		imageLookupOSDistro,
		imageLookupOSVersion,
		*mrc.machine.Spec.Version,
	)
}

func (mrc *machineReconcileContext) createTemplate(hardware *tinkv1.Hardware) error {
	if len(hardware.Spec.Disks) < 1 {
		return ErrHardwareMissingDiskConfiguration
	}

	templateData := mrc.tinkerbellMachine.Spec.TemplateOverride
	if templateData == "" {
		targetDisk := hardware.Spec.Disks[0].Device
		targetDevice := firstPartitionFromDevice(targetDisk)

		imageURL, err := mrc.imageURL()
		if err != nil {
			return fmt.Errorf("failed to generate imageURL: %w", err)
		}

		metadataIP := os.Getenv("TINKERBELL_IP")
		if metadataIP == "" {
			metadataIP = "192.168.1.1"
		}

		metadataURL := fmt.Sprintf("http://%s:50061", metadataIP)

		workflowTemplate := templates.WorkflowTemplate{
			Name:          mrc.tinkerbellMachine.Name,
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
			Name:      mrc.tinkerbellMachine.Name,
			Namespace: mrc.tinkerbellMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       mrc.tinkerbellMachine.Name,
					UID:        mrc.tinkerbellMachine.ObjectMeta.UID,
				},
			},
		},
		Spec: tinkv1.TemplateSpec{
			Data: &templateData,
		},
	}

	if err := mrc.client.Create(mrc.ctx, templateObject); err != nil {
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

func (mrc *machineReconcileContext) ensureTemplate(hardware *tinkv1.Hardware) error {
	// TODO: should this reconccile the template instead of just ensuring it exists?
	templateExists, err := mrc.templateExists()
	if err != nil {
		return fmt.Errorf("checking if Template exists: %w", err)
	}

	if templateExists {
		return nil
	}

	mrc.Log().Info("template for machine does not exist, creating")

	return mrc.createTemplate(hardware)
}

func (mrc *machineReconcileContext) takeHardwareOwnership(hardware *tinkv1.Hardware) error {
	if len(hardware.ObjectMeta.Labels) == 0 {
		hardware.ObjectMeta.Labels = map[string]string{}
	}

	hardware.ObjectMeta.Labels[HardwareOwnerNameLabel] = mrc.tinkerbellMachine.Name
	hardware.ObjectMeta.Labels[HardwareOwnerNamespaceLabel] = mrc.tinkerbellMachine.Namespace

	// Add finalizer to hardware as well to make sure we release it before Machine object is removed.
	controllerutil.AddFinalizer(hardware, infrastructurev1.MachineFinalizer)

	if err := mrc.client.Update(mrc.ctx, hardware); err != nil {
		return fmt.Errorf("updating Hardware object: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) setStatus(hardware *tinkv1.Hardware) error {
	if hardware == nil {
		hardware = &tinkv1.Hardware{}

		namespacedName := types.NamespacedName{
			Name:      mrc.tinkerbellMachine.Spec.HardwareName,
			Namespace: mrc.tinkerbellMachine.Namespace,
		}

		if err := mrc.client.Get(mrc.ctx, namespacedName, hardware); err != nil {
			return fmt.Errorf("getting Hardware: %w", err)
		}
	}

	ip, err := hardwareIP(hardware)
	if err != nil {
		return fmt.Errorf("extracting Hardware IP address: %w", err)
	}

	mrc.tinkerbellMachine.Status.Addresses = []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalIP,
			Address: ip,
		},
	}

	return mrc.patch()
}

func (mrc *machineReconcileContext) ensureHardwareUserData(hardware *tinkv1.Hardware, providerID string) error {
	userData := strings.ReplaceAll(mrc.bootstrapCloudConfig, providerIDPlaceholder, providerID)

	if hardware.Spec.UserData == nil || *hardware.Spec.UserData != userData {
		patchHelper, err := patch.NewHelper(hardware, mrc.client)
		if err != nil {
			return fmt.Errorf("initializing patch helper for selected hardware: %w", err)
		}

		hardware.Spec.UserData = &userData

		if err := patchHelper.Patch(mrc.ctx, hardware); err != nil {
			return fmt.Errorf("patching Hardware object: %w", err)
		}
	}

	return nil
}

func (mrc *machineReconcileContext) ensureHardware() (*tinkv1.Hardware, error) {
	hardware, err := mrc.hardwareForMachine()
	if err != nil {
		return nil, fmt.Errorf("getting hardware: %w", err)
	}

	if err := mrc.takeHardwareOwnership(hardware); err != nil {
		return nil, fmt.Errorf("taking Hardware ownership: %w", err)
	}

	if mrc.tinkerbellMachine.Spec.HardwareName == "" {
		mrc.log.Info("Selected Hardware for machine", "Hardware name", hardware.Name)
	}

	mrc.tinkerbellMachine.Spec.HardwareName = hardware.Name
	mrc.tinkerbellMachine.Spec.ProviderID = fmt.Sprintf("tinkerbell://%s/%s", hardware.Namespace, hardware.Name)

	if err := mrc.ensureHardwareUserData(hardware, mrc.tinkerbellMachine.Spec.ProviderID); err != nil {
		return nil, fmt.Errorf("ensuring Hardware user data: %w", err)
	}

	return hardware, mrc.setStatus(hardware)
}

func (mrc *machineReconcileContext) hardwareForMachine() (*tinkv1.Hardware, error) {
	// first query for hardware that's already assigned
	if hardware, err := mrc.assignedHardware(); err != nil {
		return nil, err
	} else if hardware != nil {
		return hardware, nil
	}

	// then fallback to searching for new hardware
	hardwareSelector := mrc.tinkerbellMachine.Spec.HardwareAffinity.DeepCopy()
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

		if err := mrc.client.List(mrc.ctx, &matched, &client.ListOptions{LabelSelector: selector}); err != nil {
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
func (mrc *machineReconcileContext) assignedHardware() (*tinkv1.Hardware, error) {
	var selectedHardware tinkv1.HardwareList
	if err := mrc.client.List(mrc.ctx, &selectedHardware, client.MatchingLabels{
		HardwareOwnerNameLabel:      mrc.tinkerbellMachine.Name,
		HardwareOwnerNamespaceLabel: mrc.tinkerbellMachine.Namespace,
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
	// compute scores for each item based on the preferred term weightss
	for _, term := range preferred {
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
func (mrc *machineReconcileContext) ensureHardwareProvisionJob(hardware *tinkv1.Hardware) error {
	if hardware.Spec.BMCRef == nil {
		mrc.log.Info("Hardware BMC reference not present; skipping BMCJob creation",
			"BMCRef", hardware.Spec.BMCRef, "Hardware", hardware.Name)

		return nil
	}

	bmcJob := &rufiov1.BMCJob{}
	jobName := fmt.Sprintf("%s-provision", mrc.tinkerbellMachine.Name)

	err := mrc.getBMCJob(jobName, bmcJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create a BMCJob for hardware provisioning
			return mrc.createHardwareProvisionJob(hardware, jobName)
		}

		return err
	}

	if bmcJob.HasCondition(rufiov1.JobFailed, rufiov1.ConditionTrue) {
		return fmt.Errorf("bmc job %s/%s failed", bmcJob.Namespace, bmcJob.Name) // nolint:goerr113
	}

	return nil
}

// getBMCJob fetches the BMCJob with name JName.
func (mrc *machineReconcileContext) getBMCJob(jName string, bmj *rufiov1.BMCJob) error {
	namespacedName := types.NamespacedName{
		Name:      jName,
		Namespace: mrc.tinkerbellMachine.Namespace,
	}

	if err := mrc.client.Get(mrc.ctx, namespacedName, bmj); err != nil {
		return fmt.Errorf("GET BMCJob: %w", err)
	}

	return nil
}

// createHardwareProvisionJob creates a BMCJob object with the required tasks for hardware provisioning.
func (mrc *machineReconcileContext) createHardwareProvisionJob(hardware *tinkv1.Hardware, name string) error {
	powerOffAction := rufiov1.HardPowerOff
	powerOnAction := rufiov1.PowerOn
	bmcJob := &rufiov1.BMCJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: mrc.tinkerbellMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       mrc.tinkerbellMachine.Name,
					UID:        mrc.tinkerbellMachine.ObjectMeta.UID,
				},
			},
		},
		Spec: rufiov1.BMCJobSpec{
			BaseboardManagementRef: rufiov1.BaseboardManagementRef{
				Name:      hardware.Spec.BMCRef.Name,
				Namespace: mrc.tinkerbellMachine.Namespace,
			},
			Tasks: []rufiov1.Task{
				{
					PowerAction: &powerOffAction,
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
					PowerAction: &powerOnAction,
				},
			},
		},
	}

	if err := mrc.client.Create(mrc.ctx, bmcJob); err != nil {
		return fmt.Errorf("creating BMCJob: %w", err)
	}

	mrc.log.Info("Created BMCJob to get hardware ready for provisioning",
		"Name", bmcJob.Name,
		"Namespace", bmcJob.Namespace)

	return nil
}

func (mrc *machineReconcileContext) getWorkflow() (*tinkv1.Workflow, error) {
	namespacedName := types.NamespacedName{
		Name:      mrc.tinkerbellMachine.Name,
		Namespace: mrc.tinkerbellMachine.Namespace,
	}

	t := &tinkv1.Workflow{}

	err := mrc.client.Get(mrc.ctx, namespacedName, t)
	if err != nil {
		msg := "failed to get workflow: %w"
		if !apierrors.IsNotFound(err) {
			msg = "no workflow exists: %w"
		}

		return t, fmt.Errorf(msg, err) // nolint:goerr113
	}

	return t, nil
}

func (mrc *machineReconcileContext) createWorkflow(hardware *tinkv1.Hardware) error {
	c := true
	workflow := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mrc.tinkerbellMachine.Name,
			Namespace: mrc.tinkerbellMachine.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       mrc.tinkerbellMachine.Name,
					UID:        mrc.tinkerbellMachine.ObjectMeta.UID,
					Controller: &c,
				},
			},
		},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: mrc.tinkerbellMachine.Name,
			HardwareMap: map[string]string{"device_1": hardware.Spec.Metadata.Instance.ID},
		},
	}

	if err := mrc.client.Create(mrc.ctx, workflow); err != nil {
		return fmt.Errorf("creating workflow: %w", err)
	}

	return nil
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
