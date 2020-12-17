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

package client_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/internal/client"
)

func TestHardwareLifecycle(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	hardwareClient := client.NewHardwareClient(realHardwareClient(t))

	// Create a Hardware resource in Tinkerbell
	testHardware, err := generateHardware(2)
	g.Expect(err).NotTo(HaveOccurred())

	hardwareMACs := make([]string, 0, len(testHardware.Network.Interfaces))
	hardwareIPs := make([]string, 0, len(testHardware.Network.Interfaces))

	for _, i := range testHardware.Network.Interfaces {
		hardwareMACs = append(hardwareMACs, i.Dhcp.Mac)
		hardwareIPs = append(hardwareIPs, i.Dhcp.Ip.Address)
	}

	g.Expect(hardwareClient.Create(ctx, testHardware)).To(Succeed())

	// Ensure that the template now has an ID set
	g.Expect(testHardware.Id).NotTo(BeEmpty())
	expectedID := testHardware.Id

	// Attempt to cleanup even if later assertions fail
	defer func() {
		// Ensure that we can delete the template by ID
		g.Expect(hardwareClient.Delete(ctx, expectedID))

		// Ensure that we now get a NotFound error trying to get the template
		_, err := hardwareClient.Get(ctx, expectedID, "", "")
		g.Expect(err).To(MatchError(client.ErrNotFound))
	}()

	// Ensure that we can get the hardware by ID and values match what we expect
	res, err := hardwareClient.Get(ctx, expectedID, "", "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res).NotTo(BeNil())
	g.Expect(res.GetId()).To(BeEquivalentTo(expectedID))
	g.Expect(res.GetMetadata()).To(BeEquivalentTo(testHardware.GetMetadata()))

	resInterfaces := res.GetNetwork().GetInterfaces()
	resMACs := make([]string, 0, len(resInterfaces))
	resIPs := make([]string, 0, len(resInterfaces))

	for _, i := range res.GetNetwork().GetInterfaces() {
		resMACs = append(resMACs, i.GetDhcp().GetMac())
		resIPs = append(resIPs, i.GetDhcp().GetIp().GetAddress())
	}

	g.Expect(resMACs).To(ConsistOf(hardwareMACs))
	g.Expect(resIPs).To(ConsistOf(hardwareIPs))

	// Ensure that we can get the hardware by mac
	for _, mac := range hardwareMACs {
		res, err := hardwareClient.Get(ctx, "", "", mac)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res).NotTo(BeNil())
		g.Expect(res.GetId()).To(BeEquivalentTo(expectedID))
	}

	// Ensure that we can get the hardware by ip
	for _, ip := range hardwareIPs {
		res, err := hardwareClient.Get(ctx, "", ip, "")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res).NotTo(BeNil())
		g.Expect(res.GetId()).To(BeEquivalentTo(expectedID))
	}

	// Ensure that we can update the hardware in Tinkerbell
	additionalInterface, err := generateHardwareInterface("")
	g.Expect(err).NotTo(HaveOccurred())

	testHardware.Network.Interfaces = append(testHardware.Network.Interfaces, additionalInterface)
	hardwareMACs = append(hardwareMACs, additionalInterface.Dhcp.Mac)
	hardwareIPs = append(hardwareIPs, additionalInterface.Dhcp.Ip.Address)

	g.Expect(hardwareClient.Update(ctx, testHardware)).To(Succeed())

	// Ensure that the hardware was updated in Tinkerbell
	res, err = hardwareClient.Get(ctx, expectedID, "", "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(res).NotTo(BeNil())
	g.Expect(res.GetId()).To(BeEquivalentTo(expectedID))
	g.Expect(res.GetMetadata()).To(BeEquivalentTo(testHardware.GetMetadata()))

	resInterfaces = res.GetNetwork().GetInterfaces()
	resMACs = make([]string, 0, len(resInterfaces))
	resIPs = make([]string, 0, len(resInterfaces))

	for _, i := range res.GetNetwork().GetInterfaces() {
		resMACs = append(resMACs, i.GetDhcp().GetMac())
		resIPs = append(resIPs, i.GetDhcp().GetIp().GetAddress())
	}

	g.Expect(resMACs).To(ConsistOf(hardwareMACs))
	g.Expect(resIPs).To(ConsistOf(hardwareIPs))
}
