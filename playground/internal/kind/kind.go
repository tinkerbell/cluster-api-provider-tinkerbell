package kind

import (
	"context"
	"fmt"
	"io"

	"github.com/tinkerbell/cluster-api-provider/playground/internal/exec"
)

const binary = "kind"

type Args struct {
	Name        string
	Kubeconfig  string
	AuditWriter io.Writer
}

func runKindClusterCommand(ctx context.Context, cmd string, c Args) error {
	args := []string{cmd, "cluster"}
	if c.Name != "" {
		args = append(args, "--name", c.Name)
	}
	if c.Kubeconfig != "" {
		args = append(args, "--kubeconfig", c.Kubeconfig)
	}
	e := exec.CommandContext(context.Background(), binary, args...)
	if c.AuditWriter != nil {
		e.AuditWriter = c.AuditWriter
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error creating kind cluster: %s: out: %v", err, string(out))
	}

	return nil
}

func CreateCluster(ctx context.Context, c Args) error {
	/*
		kind create cluster --name playground --kubeconfig /tmp/kubeconfig
	*/
	args := Args{
		Name:        c.Name,
		Kubeconfig:  c.Kubeconfig,
		AuditWriter: c.AuditWriter,
	}

	return runKindClusterCommand(ctx, "create", args)
}

func DeleteCluster(ctx context.Context, c Args) error {
	/*
		kind delete cluster --name playground
	*/
	args := Args{
		Name:        c.Name,
		AuditWriter: c.AuditWriter,
	}

	return runKindClusterCommand(ctx, "delete", args)
}
