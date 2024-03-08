package kubectl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/tinkerbell/cluster-api-provider/playground/internal/exec"
)

const binary = "kubectl"

type Args struct {
	Cmd                  string
	AdditionalPrefixArgs []string
	AdditionalSuffixArgs []string
	Kubeconfig           string
	CacheDir             string
	AuditWriter          io.Writer
}

type Opts struct {
	Kubeconfig  string
	CacheDir    string
	AuditWriter io.Writer
}

// RunCommand runs a kubectl command with the given args
func RunCommand(ctx context.Context, c Args) (string, error) {
	var args []string
	if c.CacheDir != "" {
		args = append(args, "--cache-dir", filepath.Join(c.CacheDir, ".kube", "cache"))
	}
	args = append(args, c.AdditionalPrefixArgs...)
	args = append(args, c.Cmd)
	args = append(args, c.AdditionalSuffixArgs...)

	e := exec.CommandContext(context.Background(), binary, args...)
	if c.Kubeconfig != "" {
		e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", c.Kubeconfig)}
	}
	if c.AuditWriter != nil {
		e.AuditWriter = c.AuditWriter
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run container: cmd: %v err: %w: out: %s", fmt.Sprintf("[%v %v]", binary, strings.Join(args, " ")), err, out)
	}

	return string(out), nil
}

func (o Opts) GetNodeCidrs(ctx context.Context) ([]string, error) {
	args := Args{
		Cmd:                  "get",
		AdditionalSuffixArgs: []string{"nodes", "-o", "jsonpath='{.items[*].spec.podCIDR}'"},
		Kubeconfig:           o.Kubeconfig,
		CacheDir:             o.CacheDir,
		AuditWriter:          o.AuditWriter,
	}
	out, err := RunCommand(ctx, args)
	if err != nil {
		return nil, err
	}

	cidrs := strings.Trim(string(out), "'")
	return strings.Split(cidrs, " "), nil
}

func (o Opts) ApplyFiles(ctx context.Context, files []string) error {
	formatted := []string{}
	for _, f := range files {
		formatted = append(formatted, "-f", f)
	}

	args := Args{
		Cmd:                  "apply",
		AdditionalSuffixArgs: formatted,
		Kubeconfig:           o.Kubeconfig,
		CacheDir:             o.CacheDir,
		AuditWriter:          o.AuditWriter,
	}
	_, err := RunCommand(ctx, args)
	if err != nil {
		return err
	}

	return nil
}

func generateTemplate(d any, tmpl string) (string, error) {
	t := template.New("template")
	t, err := t.Parse(tmpl)
	if err != nil {
		return "", err
	}
	buffer := new(bytes.Buffer)
	if err := t.Execute(buffer, d); err != nil {
		return "", err
	}

	return buffer.String(), nil
}

type OSImage struct {
	Registry string
	Distro   string
	Version  string
}

func (o Opts) KustomizeClusterYaml(outputDir string, name, sshAuthKeyFile string, kustomizeYaml string, namespace string, nodeLabel string, img OSImage) error {
	/*
		kubectl kustomize -o output/playground.yaml
	*/
	// get authorized key. ignore error if file doesn't exist as authorizedKey will be "" and the template will be unchanged
	authorizedKey, _ := os.ReadFile(sshAuthKeyFile)
	authorizedKey = []byte(strings.TrimSuffix(string(authorizedKey), "\n"))
	s := struct {
		SSHAuthorizedKey string
		Namespace        string
		NodeLabel        string
		OSRegistry       string
		OSDistro         string
		OSVersion        string
	}{
		SSHAuthorizedKey: string(authorizedKey),
		Namespace:        namespace,
		NodeLabel:        nodeLabel,
		OSRegistry:       img.Registry,
		OSDistro:         img.Distro,
		OSVersion:        img.Version,
	}
	patch, err := generateTemplate(s, kustomizeYaml)
	if err != nil {
		return err
	}

	// write kustomization.yaml to output dir
	if err := os.WriteFile(filepath.Join(outputDir, "kustomization.yaml"), []byte(patch), 0644); err != nil {
		return err
	}

	args := Args{
		Cmd:                  "kustomize",
		AdditionalSuffixArgs: []string{outputDir, "-o", filepath.Join(outputDir, name+".yaml")},
		Kubeconfig:           o.Kubeconfig,
		CacheDir:             o.CacheDir,
		AuditWriter:          o.AuditWriter,
	}
	out, err := RunCommand(context.Background(), args)
	if err != nil {
		return fmt.Errorf("error running kubectl kustomize: %s: out: %v", err, out)
	}

	return nil
}
