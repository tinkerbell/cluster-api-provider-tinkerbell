package capi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	binary         = "clusterctl"
	clusterctlYaml = "clusterctl.yaml"
)

func ClusterctlYamlToDisk(outputDir string) error {
	contents := fmt.Sprintf(`providers:
  - name: "tinkerbell"
    url: "https://github.com/tinkerbell/cluster-api-provider-tinkerbell/releases/v%v/infrastructure-components.yaml"
    type: "InfrastructureProvider"`, "0.4.0")

	return os.WriteFile(filepath.Join(outputDir, clusterctlYaml), []byte(contents), 0644)
}

func ClusterctlInit(outputDir, kubeconfig, tinkerbellVIP string) error {
	/*
		TINKERBELL_IP=172.18.18.18 clusterctl --config output/clusterctl.yaml init --infrastructure tinkerbell
	*/

	args := []string{"init", "--config", filepath.Join(outputDir, clusterctlYaml), "--infrastructure", "tinkerbell"}
	e := exec.CommandContext(context.Background(), binary, args...)
	e.Env = []string{
		fmt.Sprintf("TINKERBELL_IP=%s", tinkerbellVIP),
		fmt.Sprintf("KUBECONFIG=%s", kubeconfig),
		"XDG_CONFIG_HOME=/tmp/xdg",
		"XDG_CONFIG_DIRS=/tmp/xdg",
		"XDG_STATE_HOME=/tmp/xdg",
		"XDG_CACHE_HOME=/tmp/xdg",
		"XDG_RUNTIME_DIR=/tmp/xdg",
		"XDG_DATA_HOME=/tmp/xdg",
		"XDG_DATA_DIRS=/tmp/xdg",
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running clusterctl init: %s: out: %v", err, string(out))
	}

	return nil
}

func ClusterYamlToDisk(outputDir, clusterName, namespace, cpNodeNum, workerNodeNum, k8sVer, cpVIP, podCIDR, kubeconfig string) error {
	/*
		CONTROL_PLANE_VIP=172.18.18.17 POD_CIDR=172.25.0.0/16 clusterctl generate cluster playground --config outputDir/clusterctl.yaml --kubernetes-version v1.23.5 --control-plane-machine-count=1 --worker-machine-count=2 --target-namespace=tink-system --write-to playground.yaml
	*/
	args := []string{
		"generate", "cluster", clusterName,
		"--config", filepath.Join(outputDir, "clusterctl.yaml"),
		"--kubernetes-version", fmt.Sprintf("%v", k8sVer),
		fmt.Sprintf("--control-plane-machine-count=%v", cpNodeNum),
		fmt.Sprintf("--worker-machine-count=%v", workerNodeNum),
		fmt.Sprintf("--target-namespace=%v", namespace),
		"--write-to", filepath.Join(outputDir, fmt.Sprintf("%v.yaml", clusterName)),
	}
	e := exec.CommandContext(context.Background(), binary, args...)
	e.Env = []string{
		fmt.Sprintf("CONTROL_PLANE_VIP=%s", cpVIP),
		fmt.Sprintf("POD_CIDR=%v", podCIDR),
		fmt.Sprintf("KUBECONFIG=%s", kubeconfig),
		"XDG_CONFIG_HOME=/tmp/xdg",
		"XDG_CONFIG_DIRS=/tmp/xdg",
		"XDG_STATE_HOME=/tmp/xdg",
		"XDG_CACHE_HOME=/tmp/xdg",
		"XDG_RUNTIME_DIR=/tmp/xdg",
		"XDG_DATA_HOME=/tmp/xdg",
		"XDG_DATA_DIRS=/tmp/xdg",
	}
	out, err := e.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running clusterctl generate cluster: %s: out: %v", err, string(out))
	}
	return nil
}

var KustomizeYaml = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{.Namespace}}
resources:
  - playground.yaml
patches:
  - target:
      group: infrastructure.cluster.x-k8s.io
      kind: TinkerbellMachineTemplate
      name: ".*control-plane.*"
      version: v1beta1
    patch: |- 
      - op: add
        path: /spec/template/spec
        value: 
          hardwareAffinity:
            required:
            - labelSelector:
                matchLabels:
                  {{ .NodeLabel }}: control-plane
  - target:
      group: infrastructure.cluster.x-k8s.io
      kind: TinkerbellMachineTemplate
      name: ".*worker.*"
      version: v1beta1
    patch: |- 
      - op: add
        path: /spec/template/spec
        value: 
          hardwareAffinity:
            required:
            - labelSelector:
                matchLabels:
                  {{ .NodeLabel }}: worker
{{- if .SSHAuthorizedKey }}
  - target:
      group: bootstrap.cluster.x-k8s.io
      kind: KubeadmConfigTemplate
      name: "playground-.*"
      version: v1beta1
    patch: |-
      - op: add
        path: /spec/template/spec/users
        value:
          - name: tink
            sudo: ALL=(ALL) NOPASSWD:ALL
            sshAuthorizedKeys:
            - {{ .SSHAuthorizedKey }}
  - target:
      group: controlplane.cluster.x-k8s.io
      kind: KubeadmControlPlane
      name: "playground-.*"
      version: v1beta1
    patch: |-
      - op: add
        path: /spec/kubeadmConfigSpec/users
        value:
          - name: tink
            sudo: ALL=(ALL) NOPASSWD:ALL
            sshAuthorizedKeys:
            - {{ .SSHAuthorizedKey }}
{{ end -}}
`
