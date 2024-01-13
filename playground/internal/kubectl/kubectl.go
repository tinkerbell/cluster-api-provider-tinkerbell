package kubectl

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const binary = "kubectl"

type Args struct {
	Cmd                  string
	AdditionalPrefixArgs []string
	AdditionalSuffixArgs []string
	Kubeconfig           string
}

// RunCommand runs a kubectl command with the given args
func RunCommand(ctx context.Context, c Args) (string, error) {
	args := []string{c.Cmd}
	args = append(args, c.AdditionalPrefixArgs...)
	args = append(args, c.AdditionalSuffixArgs...)

	e := exec.CommandContext(context.Background(), binary, args...)
	if c.Kubeconfig != "" {
		e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", c.Kubeconfig)}
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run container: cmd: %v err: %w: out: %s", fmt.Sprintf("[%v %v]", binary, strings.Join(args, " ")), err, out)
	}

	return string(out), nil
}

func GetNodeCidrs(ctx context.Context, kubeconfig string) ([]string, error) {
	args := Args{
		Cmd:                  "get",
		AdditionalPrefixArgs: []string{"nodes", "-o", "jsonpath={.items[*].spec.podCIDR}"},
		Kubeconfig:           kubeconfig,
	}
	out, err := RunCommand(ctx, args)
	if err != nil {
		return nil, err
	}

	cidrs := strings.Trim(string(out), "'")
	return strings.Split(cidrs, " "), nil
}

func ApplyFiles(ctx context.Context, kubeconfig string, files []string) error {
	formatted := []string{}
	for _, f := range files {
		formatted = append(formatted, "-f", f)
	}

	args := Args{
		Cmd:                  "apply",
		AdditionalPrefixArgs: formatted,
		Kubeconfig:           kubeconfig,
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

func KustomizeClusterYaml(outputDir string, name, kubeconfig string, sshAuthKeyFile string, kustomizeYaml string, namespace string, nodeLabel string) error {
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
	}{
		SSHAuthorizedKey: string(authorizedKey),
		Namespace:        namespace,
		NodeLabel:        nodeLabel,
	}
	patch, err := generateTemplate(s, kustomizeYaml)
	if err != nil {
		return err
	}

	// write kustomization.yaml to output dir
	if err := os.WriteFile(filepath.Join(outputDir, "kustomization.yaml"), []byte(patch), 0644); err != nil {
		return err
	}
	cmd := "kubectl"
	args := []string{"kustomize", outputDir, "-o", filepath.Join(outputDir, name+".yaml")}
	e := exec.CommandContext(context.Background(), cmd, args...)
	e.Env = []string{fmt.Sprintf("KUBECONFIG=%s", kubeconfig)}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running kubectl kustomize: %s: out: %v", err, string(out))
	}

	return nil
}
