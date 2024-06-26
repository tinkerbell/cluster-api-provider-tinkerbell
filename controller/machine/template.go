package machine

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/internal/templates"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

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

func (scope *machineReconcileScope) createTemplate(hw *tinkv1.Hardware) error {
	if len(hw.Spec.Disks) < 1 {
		return ErrHardwareMissingDiskConfiguration
	}

	templateData := scope.tinkerbellMachine.Spec.TemplateOverride
	if templateData == "" {
		targetDisk := hw.Spec.Disks[0].Device
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

func imageURL(imageFormat, baseRegistry, osDistro, osVersion, kubernetesVersion string) (string, error) {
	type image struct {
		BaseRegistry      string
		OSDistro          string
		OSVersion         string
		KubernetesVersion string
	}
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
