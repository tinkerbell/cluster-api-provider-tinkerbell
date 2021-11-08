// Package controllers contains controller for PBNJ BMC.
package controllers

import (
	"context"
	"fmt"

	v1 "github.com/tinkerbell/pbnj/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	if bmc.Status.State == pbnjv1alpha1.BMCPowerOn {
		return ctrl.Result{}, nil
	}

	return r.reconcileNormal(ctx, bmc)
}

func (r *Reconciler) reconcileNormal(ctx context.Context, bmc *pbnjv1alpha1.BMC) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("bmc", bmc.Name)

	// Power on the machine with bmc.
	powerRequest := &v1.PowerRequest{
		Authn: &v1.Authn{
			Authn: &v1.Authn_DirectAuthn{
				DirectAuthn: &v1.DirectAuthn{
					Host: &v1.Host{
						Host: bmc.Spec.Host,
					},
					Username: bmc.Spec.Username,
					Password: bmc.Spec.Password,
				},
			},
		},
		Vendor: &v1.Vendor{
			Name: bmc.Spec.Vendor,
		},
		PowerAction: v1.PowerAction_POWER_ACTION_ON,
	}

	_, err := r.PbnjClient.MachinePower(ctx, powerRequest)
	if err != nil {
		logger.Error(err, "Failed to power on machine with bmc")

		return ctrl.Result{}, fmt.Errorf("error calling MachinePower: %w", err)
	}

	return r.reconcileStatus(ctx, bmc)
}

func (r *Reconciler) reconcileStatus(ctx context.Context, bmc *pbnjv1alpha1.BMC) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("bmc", bmc.Name)
	patch := client.MergeFrom(bmc.DeepCopy())

	bmc.Status.State = pbnjv1alpha1.BMCPowerOn
	if err := r.Client.Status().Patch(ctx, bmc, patch); err != nil {
		logger.Error(err, "Failed to patch bmc")

		return ctrl.Result{}, fmt.Errorf("failed to patch bmc: %w", err)
	}

	return ctrl.Result{}, nil
}
