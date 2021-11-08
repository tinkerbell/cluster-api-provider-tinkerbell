// Package client contains a client wrapper for PBNJ.
package client

import (
	"context"
	"fmt"
	"os"

	v1 "github.com/tinkerbell/pbnj/api/v1"
	v1Client "github.com/tinkerbell/pbnj/client"
	"google.golang.org/grpc"
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

	conn, err := grpc.Dial(grpcAuthority, grpc.WithInsecure())
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
