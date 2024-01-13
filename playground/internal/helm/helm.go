package helm

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
)

const binary = "helm"

type Args struct {
	Cmd             string
	ReleaseName     string
	Chart           *url.URL
	Version         string
	CreateNamespace bool
	Namespace       string
	Wait            bool
	SetArgs         map[string]string
	Kubeconfig      string
}

func Install(ctx context.Context, a Args) error {
	args := []string{"install", a.ReleaseName, a.Chart.String()}
	if a.Version != "" {
		args = append(args, "--version", a.Version)
	}
	if a.CreateNamespace {
		args = append(args, "--create-namespace")
	}
	if a.Namespace != "" {
		args = append(args, "--namespace", a.Namespace)
	}
	if a.Wait {
		args = append(args, "--wait")
	}
	for k, v := range a.SetArgs {
		args = append(args, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	e := exec.CommandContext(context.Background(), binary, args...)
	e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", a.Kubeconfig)}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error deploying Tinkerbell stack: %s: out: %v", err, string(out))
	}

	return nil
}
