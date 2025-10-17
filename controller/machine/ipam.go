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

package machine

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-api/api/v1beta1"
	ipamv1 "sigs.k8s.io/cluster-api/exp/ipam/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
)

const (
	// IPAMClaimFinalizer is added to IPAddressClaim to ensure proper cleanup.
	IPAMClaimFinalizer = "tinkerbellmachine.infrastructure.cluster.x-k8s.io/ipam-claim"

	// IPAMClaimNameFormat is the format for generating IPAddressClaim names.
	// Format: <machine-name>-<interface-index>.
	IPAMClaimNameFormat = "%s-%d"
)

var (
	// ErrHardwareNoInterfaces indicates that hardware has no network interfaces configured.
	ErrHardwareNoInterfaces = errors.New("hardware has no interfaces")
	// ErrHardwareNoDHCPConfig indicates that hardware's first interface has no DHCP configuration.
	ErrHardwareNoDHCPConfig = errors.New("hardware's first interface has no DHCP configuration")
	// ErrIPAMClaimNoAddressRef indicates that an IPAddressClaim has no address reference.
	ErrIPAMClaimNoAddressRef = errors.New("IPAddressClaim has no address reference")
)

// hardwareHasIPConfigured checks if the Hardware already has an IP address configured
// on its first interface.
func hardwareHasIPConfigured(hw *tinkv1.Hardware) bool {
	return len(hw.Spec.Interfaces) > 0 &&
		hw.Spec.Interfaces[0].DHCP != nil &&
		hw.Spec.Interfaces[0].DHCP.IP != nil &&
		hw.Spec.Interfaces[0].DHCP.IP.Address != ""
}

// ensureIPAddressClaim creates or retrieves an IPAddressClaim for the machine.
// It returns the claim and a boolean indicating if the IP has been allocated.
// If the Hardware already has an IP address configured on its first interface,
// IPAM is skipped to avoid conflicts with pre-existing manual configuration.
func (scope *machineReconcileScope) ensureIPAddressClaim(hw *tinkv1.Hardware, poolRef *corev1.TypedLocalObjectReference) (*ipamv1.IPAddressClaim, bool, error) {
	if poolRef == nil {
		// No IPAM pool configured, skip IPAM
		return nil, false, nil
	}

	// Check if Hardware already has an IP configured on the first interface
	// If it does, skip IPAM to avoid conflicts with manual configuration
	if hardwareHasIPConfigured(hw) {
		scope.log.Info("Hardware already has IP configured, skipping IPAM",
			"hardware", hw.Name,
			"ip", hw.Spec.Interfaces[0].DHCP.IP.Address)
		// Return nil claim but indicate "allocated" to skip further IPAM processing
		return nil, true, nil
	}

	claimName := fmt.Sprintf(IPAMClaimNameFormat, scope.tinkerbellMachine.Name, 0)

	claim := &ipamv1.IPAddressClaim{}
	claimKey := client.ObjectKey{
		Name:      claimName,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	err := scope.client.Get(scope.ctx, claimKey, claim)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, false, fmt.Errorf("failed to get IPAddressClaim: %w", err)
	}

	// Create the claim if it doesn't exist
	if apierrors.IsNotFound(err) {
		claim, err = scope.createIPAddressClaim(claimName, poolRef)
		if err != nil {
			return nil, false, fmt.Errorf("failed to create IPAddressClaim: %w", err)
		}
		scope.log.Info("Created IPAddressClaim", "claim", claimName)
		return claim, false, nil
	}

	// Check if the claim has been fulfilled
	if claim.Status.AddressRef.Name == "" {
		scope.log.Info("Waiting for IPAddressClaim to be fulfilled", "claim", claimName)
		return claim, false, nil
	}

	return claim, true, nil
}

// createIPAddressClaim creates a new IPAddressClaim for the machine.
func (scope *machineReconcileScope) createIPAddressClaim(name string, poolRef *corev1.TypedLocalObjectReference) (*ipamv1.IPAddressClaim, error) {
	claim := &ipamv1.IPAddressClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: scope.tinkerbellMachine.Namespace,
			Labels: map[string]string{
				v1beta1.ClusterNameLabel: scope.machine.Spec.ClusterName,
			},
			Finalizers: []string{IPAMClaimFinalizer},
		},
		Spec: ipamv1.IPAddressClaimSpec{
			PoolRef: *poolRef,
		},
	}

	// Set owner reference to TinkerbellMachine with controller=true for clusterctl move support
	if err := controllerutil.SetControllerReference(scope.tinkerbellMachine, claim, scope.client.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to set owner reference: %w", err)
	}

	if err := scope.client.Create(scope.ctx, claim); err != nil {
		return nil, fmt.Errorf("failed to create IPAddressClaim: %w", err)
	}

	return claim, nil
}

// getIPAddressFromClaim fetches the IPAddress resource referenced by the claim.
func (scope *machineReconcileScope) getIPAddressFromClaim(claim *ipamv1.IPAddressClaim) (*ipamv1.IPAddress, error) {
	if claim.Status.AddressRef.Name == "" {
		return nil, ErrIPAMClaimNoAddressRef
	}

	ipAddress := &ipamv1.IPAddress{}
	ipAddressKey := client.ObjectKey{
		Name:      claim.Status.AddressRef.Name,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	if err := scope.client.Get(scope.ctx, ipAddressKey, ipAddress); err != nil {
		return nil, fmt.Errorf("failed to get IPAddress: %w", err)
	}

	return ipAddress, nil
}

// patchHardwareWithIPAMAddress updates the Hardware's first interface with the allocated IP address.
func (scope *machineReconcileScope) patchHardwareWithIPAMAddress(hw *tinkv1.Hardware, ipAddr *ipamv1.IPAddress) error {
	if len(hw.Spec.Interfaces) == 0 {
		return ErrHardwareNoInterfaces
	}
	if hw.Spec.Interfaces[0].DHCP == nil {
		return ErrHardwareNoDHCPConfig
	}

	// Parse the IP address and related information
	address := ipAddr.Spec.Address
	prefix := ipAddr.Spec.Prefix
	gateway := ipAddr.Spec.Gateway

	// Update the DHCP IP configuration
	if hw.Spec.Interfaces[0].DHCP.IP == nil {
		hw.Spec.Interfaces[0].DHCP.IP = &tinkv1.IP{}
	}

	hw.Spec.Interfaces[0].DHCP.IP.Address = address

	// Set netmask if prefix is provided
	if prefix > 0 {
		netmask := prefixToNetmask(prefix)
		hw.Spec.Interfaces[0].DHCP.IP.Netmask = netmask
	}

	// Set gateway if provided
	if gateway != "" {
		hw.Spec.Interfaces[0].DHCP.IP.Gateway = gateway
	}

	// Update the Hardware resource
	if err := scope.client.Update(scope.ctx, hw); err != nil {
		return fmt.Errorf("failed to update Hardware with IPAM address: %w", err)
	}

	scope.log.Info("Updated Hardware with IPAM allocated IP",
		"hardware", hw.Name,
		"address", address,
		"prefix", prefix,
		"gateway", gateway)

	return nil
}

// deleteIPAddressClaim removes the IPAddressClaim when the machine is being deleted.
func (scope *machineReconcileScope) deleteIPAddressClaim() error {
	claimName := fmt.Sprintf(IPAMClaimNameFormat, scope.tinkerbellMachine.Name, 0)
	claim := &ipamv1.IPAddressClaim{}
	claimKey := client.ObjectKey{
		Name:      claimName,
		Namespace: scope.tinkerbellMachine.Namespace,
	}

	err := scope.client.Get(scope.ctx, claimKey, claim)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Claim already deleted
			return nil
		}
		return fmt.Errorf("failed to get IPAddressClaim for deletion: %w", err)
	}

	// Remove finalizer to allow deletion
	controllerutil.RemoveFinalizer(claim, IPAMClaimFinalizer)
	if err := scope.client.Update(scope.ctx, claim); err != nil {
		return fmt.Errorf("failed to remove finalizer from IPAddressClaim: %w", err)
	}

	// Delete the claim
	if err := scope.client.Delete(scope.ctx, claim); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete IPAddressClaim: %w", err)
	}

	scope.log.Info("Deleted IPAddressClaim", "claim", claimName)
	return nil
}

// prefixToNetmask converts a CIDR prefix length to a netmask string.
// For example, 24 -> "255.255.255.0".
func prefixToNetmask(prefix int) string {
	if prefix < 0 || prefix > 32 {
		return ""
	}

	var mask uint32 = 0xFFFFFFFF << (32 - prefix)
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(mask>>24),
		byte(mask>>16),
		byte(mask>>8),
		byte(mask))
}

// getIPAMPoolRef extracts the IPAM pool reference from the TinkerbellMachine spec.
// Returns nil if no pool is configured.
func (scope *machineReconcileScope) getIPAMPoolRef() *corev1.TypedLocalObjectReference {
	return scope.tinkerbellMachine.Spec.IPAMPoolRef
}

// reconcileIPAM handles the IPAM reconciliation for the machine.
// It creates an IPAddressClaim, waits for allocation, and updates the Hardware.
func (scope *machineReconcileScope) reconcileIPAM(hw *tinkv1.Hardware, poolRef *corev1.TypedLocalObjectReference) error {
	// Ensure IPAddressClaim exists
	claim, allocated, err := scope.ensureIPAddressClaim(hw, poolRef)
	if err != nil {
		return fmt.Errorf("failed to ensure IPAddressClaim: %w", err)
	}

	if !allocated {
		// IP not yet allocated, requeue
		scope.log.Info("Waiting for IPAM to allocate IP address")
		return nil
	}

	// Get the allocated IPAddress
	ipAddress, err := scope.getIPAddressFromClaim(claim)
	if err != nil {
		return fmt.Errorf("failed to get IPAddress from claim: %w", err)
	}

	// Check if Hardware already has this IP configured
	if len(hw.Spec.Interfaces) > 0 &&
		hw.Spec.Interfaces[0].DHCP != nil &&
		hw.Spec.Interfaces[0].DHCP.IP != nil &&
		hw.Spec.Interfaces[0].DHCP.IP.Address == ipAddress.Spec.Address {
		// IP already configured, nothing to do
		return nil
	}

	// Update Hardware with the allocated IP
	if err := scope.patchHardwareWithIPAMAddress(hw, ipAddress); err != nil {
		return fmt.Errorf("failed to patch Hardware with IPAM address: %w", err)
	}

	scope.log.Info("Successfully configured Hardware with IPAM allocated IP",
		"address", ipAddress.Spec.Address)

	return nil
}
