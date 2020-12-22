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

// Package hardware contains controller for Tinkerbell Hardware.
package hardware

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	"github.com/tinkerbell/tink/protos/hardware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type hardwareClient interface {
	// Create(ctx context.Context, h *hardware.Hardware) error
	// Update(ctx context.Context, h *hardware.Hardware) error
	Get(ctx context.Context, id, ip, mac string) (*hardware.Hardware, error)
	// Delete(ctx context.Context, id string) error
}

// Reconciler implements Reconciler interface by managing Tinkerbell hardware.
type Reconciler struct {
	client.Client
	HardwareClient hardwareClient
	Log            logr.Logger
	Scheme         *runtime.Scheme
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, hwChan <-chan event.GenericEvent) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tinkv1alpha1.Hardware{}).
		Watches(
			&source.Channel{Source: hwChan},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=hardware;hardware/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell hardware.
func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("hardware", req.NamespacedName.Name)

	// Fetch the hardware.
	hardware := &tinkv1alpha1.Hardware{}
	if err := r.Get(ctx, req.NamespacedName, hardware); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get hardware")

		return ctrl.Result{}, fmt.Errorf("failed to get hardware: %w", err)
	}

	// Deletion is a noop.
	if !hardware.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.reconcileNormal(ctx, hardware)
}

func (r *Reconciler) reconcileNormal(ctx context.Context, h *tinkv1alpha1.Hardware) (ctrl.Result, error) {
	logger := r.Log.WithValues("hardware", h.Name)

	tinkHardware, err := r.HardwareClient.Get(ctx, h.Spec.ID, "", "")
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Mark the hardware as being in an error state if it's not present in Tinkerbell
			patch := client.MergeFrom(h.DeepCopy())

			h.Status.State = tinkv1alpha1.HardwareError

			if err := r.Client.Patch(ctx, h, patch); err != nil {
				logger.Error(err, "Failed to patch hardware")

				return ctrl.Result{}, fmt.Errorf("failed to patch hardware: %w", err)
			}

			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get hardware from Tinkerbell")

		return ctrl.Result{}, fmt.Errorf("failed to get hardware from Tinkerbell: %w", err)
	}

	logger.Info("Found hardware in tinkerbell", "tinkHardware", tinkHardware)

	return r.reconcileStatus(ctx, h, tinkHardware)
}

func (r *Reconciler) reconcileStatus(ctx context.Context, h *tinkv1alpha1.Hardware, tinkHardware *hardware.Hardware) (ctrl.Result, error) {
	logger := r.Log.WithValues("hardware", h.Name)
	patch := client.MergeFrom(h.DeepCopy())

	h.Status.TinkMetadata = tinkHardware.GetMetadata()
	h.Status.TinkVersion = tinkHardware.GetVersion()
	h.Status.TinkInterfaces = []v1alpha1.Interface{}

	for _, iface := range tinkHardware.GetNetwork().GetInterfaces() {
		tinkInterface := v1alpha1.Interface{}
		if netboot := iface.GetNetboot(); netboot != nil {
			tinkInterface.Netboot = &v1alpha1.Netboot{
				AllowPXE:      pointer.BoolPtr(netboot.GetAllowPxe()),
				AllowWorkflow: pointer.BoolPtr(netboot.GetAllowWorkflow()),
			}
			if ipxe := netboot.GetIpxe(); ipxe != nil {
				tinkInterface.Netboot.IPXE = &v1alpha1.IPXE{
					URL:      ipxe.GetUrl(),
					Contents: ipxe.GetContents(),
				}
			}

			if osie := netboot.GetOsie(); osie != nil {
				tinkInterface.Netboot.OSIE = &v1alpha1.OSIE{
					BaseURL: osie.GetBaseUrl(),
					Kernel:  osie.GetKernel(),
					Initrd:  osie.GetInitrd(),
				}
			}
		}

		if dhcp := iface.GetDhcp(); dhcp != nil {
			tinkInterface.DHCP = &v1alpha1.DHCP{
				MAC:         dhcp.GetMac(),
				Hostname:    dhcp.GetHostname(),
				LeaseTime:   dhcp.GetLeaseTime(),
				NameServers: dhcp.GetNameServers(),
				TimeServers: dhcp.GetTimeServers(),
				Arch:        dhcp.GetArch(),
				UEFI:        dhcp.GetUefi(),
				IfaceName:   dhcp.GetIfaceName(),
			}

			if ip := dhcp.GetIp(); ip != nil {
				tinkInterface.DHCP.IP = &v1alpha1.IP{
					Address: ip.GetAddress(),
					Netmask: ip.GetNetmask(),
					Gateway: ip.GetGateway(),
					Family:  ip.GetFamily(),
				}
			}
		}

		h.Status.TinkInterfaces = append(h.Status.TinkInterfaces, tinkInterface)
	}

	h.Status.State = v1alpha1.HardwareReady

	if err := r.Client.Patch(ctx, h, patch); err != nil {
		logger.Error(err, "Failed to patch hardware")

		return ctrl.Result{}, fmt.Errorf("failed to patch hardware: %w", err)
	}

	return ctrl.Result{}, nil
}
