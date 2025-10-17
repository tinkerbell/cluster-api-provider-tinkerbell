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
	"testing"

	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ipamv1 "sigs.k8s.io/cluster-api/exp/ipam/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controller"
)

func Test_prefixToNetmask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix int
		want   string
	}{
		{
			name:   "CIDR /24",
			prefix: 24,
			want:   "255.255.255.0",
		},
		{
			name:   "CIDR /16",
			prefix: 16,
			want:   "255.255.0.0",
		},
		{
			name:   "CIDR /8",
			prefix: 8,
			want:   "255.0.0.0",
		},
		{
			name:   "CIDR /32",
			prefix: 32,
			want:   "255.255.255.255",
		},
		{
			name:   "CIDR /0",
			prefix: 0,
			want:   "0.0.0.0",
		},
		{
			name:   "Invalid negative",
			prefix: -1,
			want:   "",
		},
		{
			name:   "Invalid too large",
			prefix: 33,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			got := prefixToNetmask(tt.prefix)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

//nolint:funlen // test table is intentionally comprehensive
func Test_patchHardwareWithIPAMAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		hardware    *tinkv1.Hardware
		ipAddress   *ipamv1.IPAddress
		wantErr     bool
		wantAddress string
		wantNetmask string
		wantGateway string
	}{
		{
			name: "successful update with full IP information",
			hardware: &tinkv1.Hardware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hw",
					Namespace: "default",
				},
				Spec: tinkv1.HardwareSpec{
					Interfaces: []tinkv1.Interface{
						{
							DHCP: &tinkv1.DHCP{
								MAC: "00:00:00:00:00:01",
							},
						},
					},
				},
			},
			ipAddress: &ipamv1.IPAddress{
				Spec: ipamv1.IPAddressSpec{
					Address: "192.168.1.100",
					Prefix:  24,
					Gateway: "192.168.1.1",
				},
			},
			wantErr:     false,
			wantAddress: "192.168.1.100",
			wantNetmask: "255.255.255.0",
			wantGateway: "192.168.1.1",
		},
		{
			name: "update without prefix",
			hardware: &tinkv1.Hardware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hw",
					Namespace: "default",
				},
				Spec: tinkv1.HardwareSpec{
					Interfaces: []tinkv1.Interface{
						{
							DHCP: &tinkv1.DHCP{
								MAC: "00:00:00:00:00:01",
								IP:  &tinkv1.IP{},
							},
						},
					},
				},
			},
			ipAddress: &ipamv1.IPAddress{
				Spec: ipamv1.IPAddressSpec{
					Address: "10.0.0.5",
					Gateway: "10.0.0.1",
				},
			},
			wantErr:     false,
			wantAddress: "10.0.0.5",
			wantNetmask: "",
			wantGateway: "10.0.0.1",
		},
		{
			name: "hardware with no interfaces",
			hardware: &tinkv1.Hardware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hw",
					Namespace: "default",
				},
				Spec: tinkv1.HardwareSpec{
					Interfaces: []tinkv1.Interface{},
				},
			},
			ipAddress: &ipamv1.IPAddress{
				Spec: ipamv1.IPAddressSpec{
					Address: "192.168.1.100",
				},
			},
			wantErr: true,
		},
		{
			name: "hardware with no DHCP config",
			hardware: &tinkv1.Hardware{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-hw",
					Namespace: "default",
				},
				Spec: tinkv1.HardwareSpec{
					Interfaces: []tinkv1.Interface{
						{
							DHCP: nil,
						},
					},
				},
			},
			ipAddress: &ipamv1.IPAddress{
				Spec: ipamv1.IPAddressSpec{
					Address: "192.168.1.100",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			scheme := runtime.NewScheme()
			_ = controller.AddToSchemeTinkerbell(scheme)
			_ = infrastructurev1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.hardware).
				Build()

			scope := &machineReconcileScope{
				client: fakeClient,
			}

			err := scope.patchHardwareWithIPAMAddress(tt.hardware, tt.ipAddress)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(tt.hardware.Spec.Interfaces[0].DHCP.IP).ToNot(BeNil())
			g.Expect(tt.hardware.Spec.Interfaces[0].DHCP.IP.Address).To(Equal(tt.wantAddress))

			if tt.wantNetmask != "" {
				g.Expect(tt.hardware.Spec.Interfaces[0].DHCP.IP.Netmask).To(Equal(tt.wantNetmask))
			}

			if tt.wantGateway != "" {
				g.Expect(tt.hardware.Spec.Interfaces[0].DHCP.IP.Gateway).To(Equal(tt.wantGateway))
			}
		})
	}
}

func Test_getIPAMPoolRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		tinkerbellMachine *infrastructurev1.TinkerbellMachine
		want              *corev1.TypedLocalObjectReference
	}{
		{
			name: "pool ref is set",
			tinkerbellMachine: &infrastructurev1.TinkerbellMachine{
				Spec: infrastructurev1.TinkerbellMachineSpec{
					IPAMPoolRef: &corev1.TypedLocalObjectReference{
						APIGroup: stringPtr("ipam.cluster.x-k8s.io"),
						Kind:     "InClusterIPPool",
						Name:     "test-pool",
					},
				},
			},
			want: &corev1.TypedLocalObjectReference{
				APIGroup: stringPtr("ipam.cluster.x-k8s.io"),
				Kind:     "InClusterIPPool",
				Name:     "test-pool",
			},
		},
		{
			name: "pool ref is nil",
			tinkerbellMachine: &infrastructurev1.TinkerbellMachine{
				Spec: infrastructurev1.TinkerbellMachineSpec{
					IPAMPoolRef: nil,
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			scope := &machineReconcileScope{
				tinkerbellMachine: tt.tinkerbellMachine,
			}

			got := scope.getIPAMPoolRef()
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func stringPtr(s string) *string {
	return &s
}
