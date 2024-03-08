package helm

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"

	"github.com/tinkerbell/cluster-api-provider/playground/internal/exec"
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
	CacheDir        string
	AuditWriter     io.Writer
}

func Install(ctx context.Context, a Args) error {
	var args []string
	if a.CacheDir != "" {
		args = append(args, "--kubeconfig", a.Kubeconfig, "--repository-cache", filepath.Join(a.CacheDir, ".helm", "cache"))
	}
	args = append(args, "install", a.ReleaseName, a.Chart.String())
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
	e.Env = []string{fmt.Sprintf(
		"KUBECONFIG=%s", a.Kubeconfig),
		"XDG_CONFIG_HOME=/tmp/xdg",
		"XDG_CONFIG_DIRS=/tmp/xdg",
		"XDG_STATE_HOME=/tmp/xdg",
		"XDG_CACHE_HOME=/tmp/xdg",
		"XDG_RUNTIME_DIR=/tmp/xdg",
		"XDG_DATA_HOME=/tmp/xdg",
		"XDG_DATA_DIRS=/tmp/xdg",
		// Helm shells out to kubectl, which uses $HOME/.kube/config, which ends up being
		// ./.kube/config. We want this cache in the cache directory.
		fmt.Sprintf("HOME=%s", a.CacheDir),
	}
	if a.AuditWriter != nil {
		e.AuditWriter = a.AuditWriter
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error deploying Tinkerbell stack: %s: out: %v", err, string(out))
	}

	return nil
}
