package kind

import (
	"context"
	"fmt"
	"os/exec"
)

const binary = "kind"

type Args struct {
	Name       string
	Kubeconfig string
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
		Name:       c.Name,
		Kubeconfig: c.Kubeconfig,
	}

	return runKindClusterCommand(ctx, "create", args)
}

func DeleteCluster(ctx context.Context, c Args) error {
	/*
		kind delete cluster --name playground
	*/
	args := Args{
		Name: c.Name,
	}

	return runKindClusterCommand(ctx, "delete", args)
}
