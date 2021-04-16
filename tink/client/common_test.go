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
	"os"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/tinkerbell/tink/client"
	"github.com/tinkerbell/tink/protos/hardware"
	"github.com/tinkerbell/tink/protos/template"
	"github.com/tinkerbell/tink/protos/workflow"
	"google.golang.org/grpc"
)

func realConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	g := NewWithT(t)

	certURL, ok := os.LookupEnv("TINKERBELL_CERT_URL")
	if !ok || certURL == "" {
		t.Skip("Skipping live client tests because TINKERBELL_CERT_URL is not set.")
	}

	grpcAuthority, ok := os.LookupEnv("TINKERBELL_GRPC_AUTHORITY")
	if !ok || grpcAuthority == "" {
		t.Skip("Skipping live client tests because TINKERBELL_GRPC_AUTHORITY is not set.")
	}

	conn, err := client.GetConnection()
	g.Expect(err).NotTo(HaveOccurred())

	return conn
}

func realTemplateClient(t *testing.T) template.TemplateServiceClient {
	t.Helper()
	conn := realConn(t)

	return template.NewTemplateServiceClient(conn)
}

func realWorkflowClient(t *testing.T) workflow.WorkflowServiceClient {
	t.Helper()
	conn := realConn(t)

	return workflow.NewWorkflowServiceClient(conn)
}

func realHardwareClient(t *testing.T) hardware.HardwareServiceClient {
	t.Helper()
	conn := realConn(t)

	return hardware.NewHardwareServiceClient(conn)
}
