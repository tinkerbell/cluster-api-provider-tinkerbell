package machine

import (
	"cmp"
	"fmt"

	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	// ErrTemplateOverrideRefNoData is returned when a Template referenced by TemplateOverrideRef has no spec.data.
	ErrTemplateOverrideRefNoData = fmt.Errorf("template referenced by cluster TemplateOverrideRef has no spec.data")

	// ErrNoTemplateFound is returned when no template source is configured at any level.
	ErrNoTemplateFound = fmt.Errorf("no template found: set templateOverride on TinkerbellMachine, " +
		"hardware annotation %q, or templateOverride/templateOverrideRef on TinkerbellCluster", HardwareTemplateOverrideAnnotation)
)

func (scope *machineReconcileScope) templateExists() (bool, error) {
	namespacedName := types.NamespacedName{
		Name:      scope.tinkerbellMachine.Name,
		Namespace: scope.tinkerbellNamespace(),
	}

	err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, &tinkv1.Template{})
	if err == nil {
		return true, nil
	}

	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("checking if template exists: %w", err)
	}

	return false, nil
}

func (scope *machineReconcileScope) clusterTemplateOverride() (string, error) {
	switch {
	case scope.tinkerbellCluster.Spec.TemplateOverride != "":
		return scope.tinkerbellCluster.Spec.TemplateOverride, nil
	case scope.tinkerbellCluster.Spec.TemplateOverrideRef != nil:
		ref := scope.tinkerbellCluster.Spec.TemplateOverrideRef
		refTemplate := &tinkv1.Template{}
		namespacedName := types.NamespacedName{
			Name:      ref.Name,
			Namespace: cmp.Or(ref.Namespace, scope.tinkerbellNamespace()),
		}
		if err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, refTemplate); err != nil {
			return "", fmt.Errorf("failed to get Template %q referenced by cluster TemplateOverrideRef: %w", namespacedName.String(), err)
		}
		if refTemplate.Spec.Data == nil {
			return "", fmt.Errorf("%w: %s", ErrTemplateOverrideRefNoData, namespacedName.String())
		}

		return *refTemplate.Spec.Data, nil
	}

	return "", nil
}

func (scope *machineReconcileScope) createTemplate(hw *tinkv1.Hardware) error {
	if len(hw.Spec.Disks) < 1 {
		return ErrHardwareMissingDiskConfiguration
	}

	templateData := scope.tinkerbellMachine.Spec.TemplateOverride
	if templateData == "" {
		scope.log.Info("tinkerbellMachine.Spec.TemplateOverride is empty, trying from hardware annotation")
		tmplFromAnnotation, err := scope.templateFromAnnotation(hw)
		if err != nil {
			return fmt.Errorf("failed to get template from hardware annotation: %w", err)
		}
		templateData = tmplFromAnnotation
	}

	// If still no template, try the cluster-level overrides.
	if templateData == "" {
		clusterData, err := scope.clusterTemplateOverride()
		if err != nil {
			return err
		}
		templateData = clusterData
	}

	// No template found at any level — this is an error.
	if templateData == "" {
		return ErrNoTemplateFound
	}

	templateObject := &tinkv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scope.tinkerbellMachine.Name,
			Namespace: scope.tinkerbellNamespace(),
		},
		Spec: tinkv1.TemplateSpec{
			Data: &templateData,
		},
	}

	if err := scope.setResourceOwnership(templateObject); err != nil {
		return fmt.Errorf("setting template ownership: %w", err)
	}

	if err := scope.tinkerbellClient.Create(scope.ctx, templateObject); err != nil {
		return fmt.Errorf("creating Tinkerbell template: %w", err)
	}

	return nil
}

func (scope *machineReconcileScope) templateFromAnnotation(hw *tinkv1.Hardware) (string, error) {
	templateData := ""
	// Check if hardware has an annotation 'hardware.tinkerbell.org/capt-template-override', if so,
	// use it as the name of a Template resource in the same namespace as the Hardware, load it,
	// and use it's spec.data as the template.
	scope.log.Info("hardware annotations", "annotations", hw.Annotations)
	if templateName, ok := hw.Annotations[HardwareTemplateOverrideAnnotation]; ok {
		scope.log.Info("found template override in Hardware annotation", "templateName", templateName, "namespace", hw.Namespace)
		overrideTemplate := &tinkv1.Template{}
		namespacedName := types.NamespacedName{
			Name:      templateName,
			Namespace: hw.Namespace,
		}
		if err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, overrideTemplate); err != nil {
			return "", fmt.Errorf("failed to get Template %q specified in hardware annotation: %w", templateName, err)
		}
		scope.log.V(4).Info("found template override in Hardware annotations, using it as template", "templateName", templateName)
		templateData = *overrideTemplate.Spec.Data
	}
	return templateData, nil
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
		Namespace: scope.tinkerbellNamespace(),
	}

	template := &tinkv1.Template{}

	err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, template)
	if err != nil {
		if apierrors.IsNotFound(err) {
			scope.log.Info("Template already removed", "name", namespacedName)

			return nil
		}

		return fmt.Errorf("checking if template exists: %w", err)
	}

	scope.log.Info("Removing Template", "name", namespacedName)

	if err := scope.tinkerbellClient.Delete(scope.ctx, template); err != nil {
		return fmt.Errorf("ensuring template has been removed: %w", err)
	}

	return nil
}
