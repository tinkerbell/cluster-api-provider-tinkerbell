/*
Copyright 2020 The Kubernetes Authors.

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
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/internal/templates"
	tinkv1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

const providerIDPlaceholder = "PROVIDER_ID"

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

func (mrc *machineReconcileContext) addFinalizer() error {
	controllerutil.AddFinalizer(mrc.tinkerbellMachine, infrastructurev1.MachineFinalizer)

	if err := mrc.patch(); err != nil {
		return fmt.Errorf("patching TinkerbellMachine object with finalizer: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) ensureDependencies() error {
	hardware, err := mrc.ensureHardware()
	if err != nil {
		return fmt.Errorf("ensuring hardware: %w", err)
	}

	if err := mrc.ensureTemplate(hardware); err != nil {
		return fmt.Errorf("ensuring template: %w", err)
	}

	if err := mrc.ensureWorkflow(); err != nil {
		return fmt.Errorf("ensuring workflow: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) markAsReady() error {
	mrc.tinkerbellMachine.Status.Ready = true

	if err := mrc.patch(); err != nil {
		return fmt.Errorf("patching machine with ready status: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) Reconcile() error {
	// To make sure we do not create orphaned objects.
	if err := mrc.addFinalizer(); err != nil {
		return fmt.Errorf("adding finalizer: %w", err)
	}

	if err := mrc.ensureDependencies(); err != nil {
		return fmt.Errorf("ensuring machine dependencies: %w", err)
	}

	if err := mrc.markAsReady(); err != nil {
		return fmt.Errorf("marking machine as ready: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) templateExists() (bool, error) {
	namespacedName := types.NamespacedName{
		Name: mrc.tinkerbellMachine.Name,
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
	templateData := mrc.tinkerbellMachine.Spec.TemplateOverride
	if templateData == "" {
		if len(hardware.Status.Disks) < 1 {
			return ErrHardwareMissingDiskConfiguration
		}

		targetDisk := hardware.Status.Disks[0].Device
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
			Name: mrc.tinkerbellMachine.Name,
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
	patchHelper, err := patch.NewHelper(hardware, mrc.client)
	if err != nil {
		return fmt.Errorf("initializing patch helper for selected hardware: %w", err)
	}

	if len(hardware.ObjectMeta.Labels) == 0 {
		hardware.ObjectMeta.Labels = map[string]string{}
	}

	hardware.ObjectMeta.Labels[HardwareOwnerNameLabel] = mrc.tinkerbellMachine.Name
	hardware.ObjectMeta.Labels[HardwareOwnerNamespaceLabel] = mrc.tinkerbellMachine.Namespace

	// Add finalizer to hardware as well to make sure we release it before Machine object is removed.
	controllerutil.AddFinalizer(hardware, infrastructurev1.MachineFinalizer)

	if err := patchHelper.Patch(mrc.ctx, hardware); err != nil {
		return fmt.Errorf("patching Hardware object: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) setStatus(hardware *tinkv1.Hardware) error {
	if hardware == nil {
		hardware = &tinkv1.Hardware{}

		namespacedName := types.NamespacedName{
			Name: mrc.tinkerbellMachine.Spec.HardwareName,
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
	mrc.tinkerbellMachine.Spec.ProviderID = fmt.Sprintf("tinkerbell://%s", hardware.Spec.ID)

	if err := mrc.ensureHardwareUserData(hardware, mrc.tinkerbellMachine.Spec.ProviderID); err != nil {
		return nil, fmt.Errorf("ensuring Hardware user data: %w", err)
	}

	return hardware, mrc.setStatus(hardware)
}

func (mrc *machineReconcileContext) hardwareForMachine() (*tinkv1.Hardware, error) {
	alreadySelectedHardwareSelector := []string{
		fmt.Sprintf("%s=%s", HardwareOwnerNameLabel, mrc.tinkerbellMachine.Name),
		fmt.Sprintf("%s=%s", HardwareOwnerNamespaceLabel, mrc.tinkerbellMachine.Namespace),
	}

	alreadySelectedHardware, err := nextHardware(mrc.ctx, mrc.client, alreadySelectedHardwareSelector)
	if err != nil {
		return nil, fmt.Errorf("checking if hardware has already been selected: %w", err)
	}

	// If we already selected Hardware but we failed to commit this information into TinkerbellMachine object,
	// this allows to pick up the process from where we left.
	if alreadySelectedHardware != nil {
		return alreadySelectedHardware, nil
	}

	return nextAvailableHardware(mrc.ctx, mrc.client, nil)
}

func (mrc *machineReconcileContext) workflowExists() (bool, error) {
	namespacedName := types.NamespacedName{
		Name: mrc.tinkerbellMachine.Name,
	}

	err := mrc.client.Get(mrc.ctx, namespacedName, &tinkv1.Workflow{})
	if err == nil {
		return true, nil
	}

	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("checking if workflow exists: %w", err)
	}

	return false, nil
}

func (mrc *machineReconcileContext) createWorkflow() error {
	workflow := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name: mrc.tinkerbellMachine.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "TinkerbellMachine",
					Name:       mrc.tinkerbellMachine.Name,
					UID:        mrc.tinkerbellMachine.ObjectMeta.UID,
				},
			},
		},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: mrc.tinkerbellMachine.Name,
			HardwareRef: mrc.tinkerbellMachine.Spec.HardwareName,
		},
	}

	if err := mrc.client.Create(mrc.ctx, workflow); err != nil {
		return fmt.Errorf("creating workflow: %w", err)
	}

	return nil
}

func (mrc *machineReconcileContext) ensureWorkflow() error {
	workflowExists, err := mrc.workflowExists()
	if err != nil {
		return fmt.Errorf("checking if workflow exists: %w", err)
	}

	if workflowExists {
		return nil
	}

	mrc.log.Info("Workflow does not exist, creating")

	return mrc.createWorkflow()
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
