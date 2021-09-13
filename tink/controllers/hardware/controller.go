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
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/tinkerbell/tink/protos/hardware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	tinkv1alpha1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

type hardwareClient interface {
	// Create(ctx context.Context, h *hardware.Hardware) error
	Update(ctx context.Context, h *hardware.Hardware) error
	Get(ctx context.Context, id, ip, mac string) (*hardware.Hardware, error)
	// Delete(ctx context.Context, id string) error
}

// Reconciler implements Reconciler interface by managing Tinkerbell hardware.
type Reconciler struct {
	client.Client
	HardwareClient hardwareClient
}

// SetupWithManager configures reconciler with a given manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	return ctrl.NewControllerManagedBy(mgr). //nolint:wrapcheck
							WithOptions(options).
							For(&tinkv1alpha1.Hardware{}).
							Complete(r)
}

// +kubebuilder:rbac:groups=tinkerbell.org,resources=hardware;hardware/status,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures state of Tinkerbell hardware.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("hardware", req.NamespacedName.Name)

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
	logger := ctrl.LoggerFrom(ctx).WithValues("hardware", h.Name)

	tinkHardware, err := r.HardwareClient.Get(ctx, h.Spec.ID, "", "")
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Mark the hardware as being in an error state if it's not present in Tinkerbell
			patch := client.MergeFrom(h.DeepCopy())

			h.Status.State = tinkv1alpha1.HardwareError

			if err := r.Client.Status().Patch(ctx, h, patch); err != nil {
				logger.Error(err, "Failed to patch hardware")

				return ctrl.Result{}, fmt.Errorf("failed to patch hardware: %w", err)
			}

			return ctrl.Result{}, nil
		}

		logger.Error(err, "Failed to get hardware from Tinkerbell")

		return ctrl.Result{}, fmt.Errorf("failed to get hardware from Tinkerbell: %w", err)
	}

	logger.Info("Found hardware in tinkerbell", "tinkHardware", tinkHardware)

	// TODO: also allow for reconciling hw.metadata.instance.id and hw.metadata.instance.hostname if not set?
	// TODO: bubble up storage information better in status

	if err := r.reconcileUserData(ctx, h, tinkHardware); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileStatus(ctx, h, tinkHardware)
}

func (r *Reconciler) reconcileUserData(
	ctx context.Context,
	h *tinkv1alpha1.Hardware,
	tinkHardware *hardware.Hardware,
) error {
	logger := ctrl.LoggerFrom(ctx).WithValues("hardware", h.Name)

	// if UserData is nil, skip reconciliation
	if h.Spec.UserData == nil {
		return nil
	}

	metadata := tinkHardware.GetMetadata()

	hwMetaData := make(map[string]interface{})
	if err := json.Unmarshal([]byte(metadata), &hwMetaData); err != nil {
		logger.Error(err, "Failed to unmarshal metadata from json")

		return fmt.Errorf("failed to unmarshal metadata from json: %w", err)
	}

	if hwMetaData["userdata"] != *h.Spec.UserData {
		hwMetaData["userdata"] = *h.Spec.UserData

		newHWMetaData, err := json.Marshal(hwMetaData)
		if err != nil {
			logger.Error(err, "Failed to marshal updated metadata to json")

			return fmt.Errorf("failed to marshal updated metadata to json: %w", err)
		}

		tinkHardware.Metadata = string(newHWMetaData)
		if err := r.HardwareClient.Update(ctx, tinkHardware); err != nil {
			logger.Error(err, "Failed to update hardware userdata", "hardware", tinkHardware)

			return fmt.Errorf("failed to update hardware userdata: %w", err)
		}

		logger.Info("Updated userdata for hardware in Tinkerbell")
	}

	return nil
}

func interfaceFromTinkInterface(iface *hardware.Hardware_Network_Interface) tinkv1alpha1.Interface {
	tinkInterface := tinkv1alpha1.Interface{}
	if netboot := iface.GetNetboot(); netboot != nil {
		tinkInterface.Netboot = &tinkv1alpha1.Netboot{
			AllowPXE:      pointer.BoolPtr(netboot.GetAllowPxe()),
			AllowWorkflow: pointer.BoolPtr(netboot.GetAllowWorkflow()),
		}
		if ipxe := netboot.GetIpxe(); ipxe != nil {
			tinkInterface.Netboot.IPXE = &tinkv1alpha1.IPXE{
				URL:      ipxe.GetUrl(),
				Contents: ipxe.GetContents(),
			}
		}

		if osie := netboot.GetOsie(); osie != nil {
			tinkInterface.Netboot.OSIE = &tinkv1alpha1.OSIE{
				BaseURL: osie.GetBaseUrl(),
				Kernel:  osie.GetKernel(),
				Initrd:  osie.GetInitrd(),
			}
		}
	}

	if dhcp := iface.GetDhcp(); dhcp != nil {
		tinkInterface.DHCP = &tinkv1alpha1.DHCP{
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
			tinkInterface.DHCP.IP = &tinkv1alpha1.IP{
				Address: ip.GetAddress(),
				Netmask: ip.GetNetmask(),
				Gateway: ip.GetGateway(),
				Family:  ip.GetFamily(),
			}
		}
	}

	return tinkInterface
}

func (r *Reconciler) reconcileStatus(
	ctx context.Context,
	h *tinkv1alpha1.Hardware,
	tinkHardware *hardware.Hardware,
) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("hardware", h.Name)
	patch := client.MergeFrom(h.DeepCopy())

	h.Status.TinkMetadata = tinkHardware.GetMetadata()
	h.Status.TinkVersion = tinkHardware.GetVersion()
	h.Status.Interfaces = []tinkv1alpha1.Interface{}

	for _, iface := range tinkHardware.GetNetwork().GetInterfaces() {
		tinkInterface := interfaceFromTinkInterface(iface)
		h.Status.Interfaces = append(h.Status.Interfaces, tinkInterface)
	}

	h.Status.State = tinkv1alpha1.HardwareReady

	disks, err := disksFromMetaData(h.Status.TinkMetadata)
	if err != nil {
		// TODO: better way to bubble up an issue here?
		logger.Error(err, "Failed to parse disk information from metadata")
	}

	h.Status.Disks = disks

	if err := r.Client.Status().Patch(ctx, h, patch); err != nil {
		logger.Error(err, "Failed to patch hardware")

		return ctrl.Result{}, fmt.Errorf("failed to patch hardware: %w", err)
	}

	return ctrl.Result{}, nil
}

func disksFromMetaData(metadata string) ([]tinkv1alpha1.Disk, error) {
	// Attempt to extract disk information from metadata
	hwMetaData := make(map[string]interface{})
	if err := json.Unmarshal([]byte(metadata), &hwMetaData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata from json: %w", err)
	}

	if instanceData, ok := hwMetaData["instance"]; ok {
		id := reflect.ValueOf(instanceData)
		if id.Kind() == reflect.Map && id.Type().Key().Kind() == reflect.String {
			storage := reflect.ValueOf(id.MapIndex(reflect.ValueOf("storage")).Interface())
			if storage.Kind() == reflect.Map && storage.Type().Key().Kind() == reflect.String {
				return parseDisks(storage.MapIndex(reflect.ValueOf("disks")).Interface()), nil
			}
		}
	}

	return nil, nil
}

func parseDisks(disks interface{}) []tinkv1alpha1.Disk {
	d := reflect.ValueOf(disks)
	if d.Kind() == reflect.Slice {
		foundDisks := make([]tinkv1alpha1.Disk, 0, d.Len())

		for i := 0; i < d.Len(); i++ {
			disk := reflect.ValueOf(d.Index(i).Interface())
			if disk.Kind() == reflect.Map && disk.Type().Key().Kind() == reflect.String {
				device := reflect.ValueOf(disk.MapIndex(reflect.ValueOf("device")).Interface())
				if device.Kind() == reflect.String {
					foundDisks = append(foundDisks, tinkv1alpha1.Disk{Device: device.String()})
				}
			}
		}

		return foundDisks
	}

	return nil
}
