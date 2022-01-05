// Package controllers contains controller for PBNJ BMC.
package controllers

import (
	"context"
	"fmt"

	v1 "github.com/tinkerbell/pbnj/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	pbnjv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/pbnj/api/v1alpha1"
)

type pbnjClient interface {
	MachinePower(ctx context.Context, powerRequest *v1.PowerRequest) (*v1.StatusResponse, error)
}

// Reconciler implements the Reconciler interface for managing BMC state.
type Reconciler struct {
	client.Client
	PbnjClient pbnjClient
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr). //nolint:wrapcheck
							WithOptions(options).
							For(&pbnjv1alpha1.BMC{}).
							Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=bmc;bmc/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of PBNJ BMC.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("bmc", req.NamespacedName.Name)

	// Fetch the bmc.
	bmc := &pbnjv1alpha1.BMC{}
	if err := r.Get(ctx, req.NamespacedName, bmc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get bmc")

		return ctrl.Result{}, fmt.Errorf("failed to get bmc: %w", err)
	}

	if bmc.Status.PowerState == pbnjv1alpha1.BMCState(bmc.Spec.PowerAction) {
		return ctrl.Result{}, nil
	}

	return r.reconcileNormal(ctx, bmc)
}

func (r *Reconciler) reconcileNormal(ctx context.Context, bmc *pbnjv1alpha1.BMC) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("bmc", bmc.Name)

	if bmc.Spec.PowerAction == "" {
		return r.reconcileStatus(ctx, bmc)
	}

	username, password, err := r.resolveAuthSecretRef(ctx, bmc.Spec.AuthSecretRef)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error resolving authentication from Secret: %w", err)
	}

	err = r.powerAction(ctx, username, password, bmc)
	if err != nil {
		logger.Error(err, "Failed to perform power action with bmc", "PowerAction", bmc.Spec.PowerAction)

		return ctrl.Result{}, fmt.Errorf("failed to perform PowerAction: %s", bmc.Spec.PowerAction) //nolint:goerr113
	}

	return r.reconcileStatus(ctx, bmc)
}

func (r *Reconciler) powerAction(ctx context.Context, username, password string, bmc *pbnjv1alpha1.BMC) error {
	powerActionValue, ok := v1.PowerAction_value[bmc.Spec.PowerAction]
	if !ok {
		return fmt.Errorf("invalid PowerAction: %s", bmc.Spec.PowerAction) //nolint:goerr113
	}

	powerRequest := &v1.PowerRequest{
		Authn: &v1.Authn{
			Authn: &v1.Authn_DirectAuthn{
				DirectAuthn: &v1.DirectAuthn{
					Host: &v1.Host{
						Host: bmc.Spec.Host,
					},
					Username: username,
					Password: password,
				},
			},
		},
		Vendor: &v1.Vendor{
			Name: bmc.Spec.Vendor,
		},
		PowerAction: v1.PowerAction(powerActionValue),
	}

	_, err := r.PbnjClient.MachinePower(ctx, powerRequest)
	if err != nil {
		return fmt.Errorf("error calling PBNJ MachinePower: %w", err)
	}

	return nil
}

func (r *Reconciler) resolveAuthSecretRef(ctx context.Context, secretRef corev1.SecretReference) (string, string, error) { //nolint:lll
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: secretRef.Namespace, Name: secretRef.Name}

	if err := r.Client.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", fmt.Errorf("error secret %s not found: %w", key, err)
		}

		return "", "", fmt.Errorf("failed to retrieve secret %s : %w", secretRef, err)
	}

	username, ok := secret.Data["username"]
	if !ok {
		return "", "", fmt.Errorf("non-existent secret key username") //nolint:goerr113
	}

	password, ok := secret.Data["password"]
	if !ok {
		return "", "", fmt.Errorf("non-existent secret key password") //nolint:goerr113
	}

	return string(username), string(password), nil
}

func (r *Reconciler) reconcileStatus(ctx context.Context, bmc *pbnjv1alpha1.BMC) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("bmc", bmc.Name)
	patch := client.MergeFrom(bmc.DeepCopy())

	bmc.Status.PowerState = pbnjv1alpha1.BMCState(bmc.Spec.PowerAction)
	if err := r.Client.Status().Patch(ctx, bmc, patch); err != nil {
		logger.Error(err, "Failed to patch bmc")

		return ctrl.Result{}, fmt.Errorf("failed to patch bmc: %w", err)
	}

	return ctrl.Result{}, nil
}
