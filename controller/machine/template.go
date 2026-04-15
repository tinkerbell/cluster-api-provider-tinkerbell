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
	// ErrTemplateRefNoData is returned when a Template referenced by TemplateRef has no spec.data.
	ErrTemplateRefNoData = fmt.Errorf("template referenced by TemplateRef has no spec.data")

	// ErrNoTemplateFound is returned when no template source is configured at any level.
	ErrNoTemplateFound = fmt.Errorf("no template found: set templateInline or templateRef on TinkerbellMachine, "+
		"hardware annotation %q, or templateInline/templateRef on TinkerbellCluster", HardwareTemplateOverrideAnnotation)
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

func (scope *machineReconcileScope) clusterTemplate() (string, error) {
	switch {
	case scope.tinkerbellCluster.Spec.TemplateInline != "":
		return scope.tinkerbellCluster.Spec.TemplateInline, nil
	case scope.tinkerbellCluster.Spec.TemplateRef != nil:
		ref := scope.tinkerbellCluster.Spec.TemplateRef
		refTemplate := &tinkv1.Template{}
		namespacedName := types.NamespacedName{
			Name:      ref.Name,
			Namespace: cmp.Or(ref.Namespace, scope.tinkerbellNamespace()),
		}
		if err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, refTemplate); err != nil {
			return "", fmt.Errorf("failed to get Template %q referenced by cluster TemplateRef: %w", namespacedName.String(), err)
		}
		if refTemplate.Spec.Data == nil {
			return "", fmt.Errorf("%w: %s", ErrTemplateRefNoData, namespacedName.String())
		}

		return *refTemplate.Spec.Data, nil
	}

	return "", nil
}

func (scope *machineReconcileScope) createTemplate(hw *tinkv1.Hardware) error {
	if len(hw.Spec.Disks) < 1 {
		return ErrHardwareMissingDiskConfiguration
	}

	templateData, err := scope.resolveTemplateData(hw)
	if err != nil {
		return err
	}

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

// resolveTemplateData resolves the workflow template data by checking, in order:
// machine-level inline, machine-level ref, hardware annotation, cluster-level template.
func (scope *machineReconcileScope) resolveTemplateData(hw *tinkv1.Hardware) (string, error) {
	if data := scope.tinkerbellMachine.Spec.TemplateInline; data != "" {
		return data, nil
	}

	if scope.tinkerbellMachine.Spec.TemplateRef != nil {
		ref := scope.tinkerbellMachine.Spec.TemplateRef
		refTemplate := &tinkv1.Template{}
		namespacedName := types.NamespacedName{
			Name:      ref.Name,
			Namespace: cmp.Or(ref.Namespace, scope.tinkerbellNamespace()),
		}
		if err := scope.tinkerbellClient.Get(scope.ctx, namespacedName, refTemplate); err != nil {
			return "", fmt.Errorf("failed to get Template %q referenced by machine TemplateRef: %w", namespacedName.String(), err)
		}
		if refTemplate.Spec.Data == nil {
			return "", fmt.Errorf("%w: %s", ErrTemplateRefNoData, namespacedName.String())
		}
		return *refTemplate.Spec.Data, nil
	}

	scope.log.Info("machine template fields are empty, trying from hardware annotation")
	if data, err := scope.templateFromAnnotation(hw); err != nil {
		return "", fmt.Errorf("failed to get template from hardware annotation: %w", err)
	} else if data != "" {
		return data, nil
	}

	return scope.clusterTemplate()
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
