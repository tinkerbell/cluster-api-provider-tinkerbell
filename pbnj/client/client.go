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

// Package client contains a client wrapper for PBNJ.
package client

import (
	"context"
	"fmt"
	"os"

	v1 "github.com/tinkerbell/pbnj/api/v1"
	v1Client "github.com/tinkerbell/pbnj/client"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PbnjGrpcAuthorityEnv represents the env var name for PBNJ GRPC server.
const PbnjGrpcAuthorityEnv = "PBNJ_GRPC_AUTHORITY"

// PbnjClient service client.
type PbnjClient struct {
	machineClient v1.MachineClient
	taskClient    v1.TaskClient
}

// NewPbnjClient returns a PBNJ service client.
func NewPbnjClient(mClient v1.MachineClient, tClient v1.TaskClient) *PbnjClient {
	return &PbnjClient{machineClient: mClient, taskClient: tClient}
}

// SetupConnection creates a GRPC connection to PBNJ server.
func SetupConnection() (*grpc.ClientConn, error) {
	grpcAuthority := os.Getenv(PbnjGrpcAuthorityEnv)
	if grpcAuthority == "" {
		return nil, fmt.Errorf("undefined %s", PbnjGrpcAuthorityEnv) //nolint:goerr113
	}

	conn, err := grpc.Dial(grpcAuthority, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("error connecting to pbnj server: %w", err)
	}

	return conn, nil
}

// MachinePower performs a PBNJ machine power request.
func (pc *PbnjClient) MachinePower(ctx context.Context, powerRequest *v1.PowerRequest) (*v1.StatusResponse, error) {
	response, err := v1Client.MachinePower(ctx, pc.machineClient, pc.taskClient, powerRequest)
	if err != nil {
		return nil, fmt.Errorf("error making pbnj PowerRequest with action %s: %w",
			powerRequest.GetPowerAction().String(), err)
	}

	return response, nil
}

// MachineBootDev performs a PBNJ machine boot device request.
func (pc *PbnjClient) MachineBootDev(ctx context.Context, deviceRequest *v1.DeviceRequest) (*v1.StatusResponse, error) {
	response, err := v1Client.MachineBootDev(ctx, pc.machineClient, pc.taskClient, deviceRequest)
	if err != nil {
		return nil, fmt.Errorf("error making pbnj DeviceRequest with boot device %s: %w",
			deviceRequest.GetBootDevice().String(), err)
	}

	return response, nil
}
