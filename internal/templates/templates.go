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

// Package templates provides methods for rendering templates used for
// creating Tinkerbell machines for ClusterAPI.
package templates

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"
)

// WorkflowTemplate is a helper struct for rendering CAPT Template data.
type WorkflowTemplate struct {
	Name            string
	CloudInitConfig CloudInitConfig
}

// CloudInitConfig allows building cloud-init configuration.
type CloudInitConfig struct {
	Hostname    string
	CloudConfig CloudConfig
}

// CloudConfig allows building ClusterAPI compatible cloud-config user-data.
type CloudConfig struct {
	BootstrapCloudConfig string
	KubernetesVersion    string
	SSHPublicKeys        []string
	ControlPlane         bool
	ProviderID           string
}

// render renders given configuration and returns file content, which can be placed in
// /etc/cloud/cloud.cfg.d/ directory.
func (cic CloudInitConfig) render() (string, error) {
	cloudConfig, err := cic.CloudConfig.render()
	if err != nil {
		return "", fmt.Errorf("rendering cloud-config template: %w", err)
	}

	return fmt.Sprintf(cloudInitConfigTemplate, cic.Hostname, indent(cloudConfig, "      ")), nil
}

// Render renders workflow template for a given machine including user-data.
func (wt WorkflowTemplate) Render() (string, error) {
	if wt.Name == "" {
		return "", fmt.Errorf("name can't be empty")
	}

	cloudInitConfigRaw, err := wt.CloudInitConfig.render()
	if err != nil {
		return "", fmt.Errorf("rendering cloud-config template: %w", err)
	}

	cloudConfig := base64.StdEncoding.EncodeToString([]byte(cloudInitConfigRaw))

	return fmt.Sprintf(workflowTemplate, wt.Name, wt.Name, cloudConfig), nil
}

const (
	providerIDPlaceholder = "PROVIDER_ID"

	workflowTemplate = `
version: "0.1"
name: %s
global_timeout: 1800
tasks:
  - name: "%s"
    worker: "{{.device_1}}"
    volumes:
      - /dev:/dev
      - /statedir:/statedir
    actions:
      - name: "dump-cloud-init"
        image: ubuntu-install
        command:
          - sh
          - -c
          - |
            echo '%s' | base64 -d > /statedir/90_dpkg.cfg
      - name: "download-image"
        image: ubuntu-install
        command:
          - sh
          - -c
          - |
            # TODO: Pull image from Tinkerbell nginx and convert it there, so we can pipe
            # wget directly into dd.
            /usr/bin/wget https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img \
              -O /statedir/focal-server-cloudimg-amd64.img
      - name: "write-image-to-disk"
        image: ubuntu-install
        command:
          - sh
          - -c
          - |
            /usr/bin/qemu-img convert -f qcow2 -O raw /statedir/focal-server-cloudimg-amd64.img /dev/vda
      - name: "write-cloud-init-config"
        image: ubuntu-install
        command:
          - sh
          - -c
          - |
            set -eux
            partprobe /dev/vda
            mkdir -p /mnt/target
            mount -t ext4 /dev/vda1 /mnt/target
            cp /statedir/90_dpkg.cfg /mnt/target/etc/cloud/cloud.cfg.d/
            # Those commands are required to satisfy kubeadm preflight checks.
            # We cannot put those in 'write_files' or 'runcmd' from cloud-config, as it will override
            # what kubeadm bootstrapper generates and there is no trivial way to merge with this.
            # We could put this in templates/cluster-template.yaml, but this makes is visible to the user
            # making user-facing configuration more complex and more fragile at the same time, as user may
            # remove it from the configuration.
            echo br_netfilter > /mnt/target/etc/modules-load.d/kubernetes.conf
            # Use 'tee -a' here because of Tinkerbell bug which use html/template instead of text/template and breaks
            # the script while rendering.
            echo 'net.bridge.bridge-nf-call-iptables = 1' | tee -a /mnt/target/etc/sysctl.d/99-kubernetes-cri.conf
            echo 'net.ipv4.ip_forward                = 1' | tee -a /mnt/target/etc/sysctl.d/99-kubernetes-cri.conf
            umount /mnt/target
      # This task shouldn't really be there, but there is no other way to reboot the
      # Tinkerbell Worker into target OS in Tinkerbell for now.
      - name: "reboot"
        image: ubuntu-install
        command:
          - sh
          - -c
          - |
            echo 1 > /proc/sys/kernel/sysrq; echo b > /proc/sysrq-trigger
`

	cloudConfigTemplate = `
apt:
  sources:
    kubernetes:
      # TODO: We use Xenial for Focal, but it seems upstream does not
      # publish newer pool?
      source: "deb https://apt.kubernetes.io/ kubernetes-xenial main"
      # Key from https://packages.cloud.google.com/apt/doc/apt-key.gpg
      key: |
        -----BEGIN PGP PUBLIC KEY BLOCK-----

        mQENBF/Jfl4BCADTPUXdkNu057X+P3STVxCzJpU2Mn+tUamKdSdVambGeYFINcp/
        EGwNGhdb0a1BbHs1SWYZbzwh4d6+p3k4ABzVMO+RpMu/aBx9E5aOn5c8GzHjZ/VE
        aheqLLhSUcSCzChSZcN5jz0hTGhmAGaviMt6RMzSfbIhZPj1kDzBiGd0Qwd/rOPn
        Jr4taPruR3ecBjhHti1/BMGd/lj0F7zQnCjp7PrqgpEPBT8jo9wX2wvOyXswSI/G
        sfbFiaOJfDnYengaEg8sF+u3WOs0Z20cSr6kS76KHpTfa3JjYsfHt8NDw8w4e3H8
        PwQzNiRP9tXeMASKQz3emMj/ek6HxjihY9qFABEBAAG0umdMaW51eCBSYXB0dXJl
        IEF1dG9tYXRpYyBTaWduaW5nIEtleSAoLy9kZXBvdC9nb29nbGUzL3Byb2R1Y3Rp
        b24vYm9yZy9jbG91ZC1yYXB0dXJlL2tleXMvY2xvdWQtcmFwdHVyZS1wdWJrZXlz
        L2Nsb3VkLXJhcHR1cmUtc2lnbmluZy1rZXktMjAyMC0xMi0wMy0xNl8wOF8wNS5w
        dWIpIDxnbGludXgtdGVhbUBnb29nbGUuY29tPokBKAQTAQgAHAUCX8l+XgkQi1fF
        woNvS+sCGwMFCQPDCrACGQEAAEF6CACaekro6aUJJd3mVtrtLOOewV8et1jep5ew
        mpOrew/pajRVBeIbV1awVn0/8EcenFejmP6WFcdCWouDVIS/QmRFQV9N6YXN8Piw
        alrRV3bTKFBHkwa1cEH4AafCGo0cDvJb8N3JnM/Rmb1KSGKr7ZXpmkLtYVqr6Hgz
        l+snrlH0Xwsl5r3SyvqBgvRYTQKZpKqmBEd1udieVoLSF988kKeNDjFa+Q1SjZPG
        W+XukgE8kBUbSDx8Y8q6Cszh3VVY+5JUeqimRgJ2ADY2/3lEtAZOtmwcBlhY0cPW
        Vqga14E7kTGSWKC6W96Nfy9K7L4Ypp8nTMErus181aqwwNfMqnpnuQENBF/Jfl4B
        CADDSh+KdBeNjIclVVnRKt0QT5593yF4WVZt/TgNuaEZ5vKknooVVIq+cJIfY/3l
        Uqq8Te4dEjodtFyKe5Xuego6qjzs8TYFdCAHXpXRoUolT14m+qkJ8rhSrpN0TxIj
        WJbJdm3NlrgTam5RKJw3ShypNUxyolnHelXxqyKDCkxBSDmR6xcdft3wdQl5IkIA
        wxe6nywmSUtpndGLRJdJraJiaWF2IBjFNg3vTEYj4eoehZd4XrvEyLVrMbKZ5m6f
        1o6QURuzSrUH9JT/ivZqCmhPposClXXX0bbi9K0Z/+uVyk6v76ms3O50rIq0L0Ye
        hM8G++qmGO421+0qCLkdD5/jABEBAAGJAR8EGAEIABMFAl/Jfl4JEItXxcKDb0vr
        AhsMAAAbGggAw7lhSWElZpGV1SI2b2K26PB93fVI1tQYV37WIElCJsajF+/ZDfJJ
        2d6ncuQSleH5WRccc4hZfKwysA/epqrCnwc7yKsToZ4sw8xsJF1UtQ5ENtkdArVi
        BJHS4Y2VZ5DEUmr5EghGtZFh9a6aLoeMVM/nrZCLstDVoPKEpLokHu/gebCwfT/n
        9U1dolFIovg6eKACl5xOx+rzcAVp7R4P527jffudz3dKMdLhPrstG0w5YbyfPPwW
        MOPp+kUF45eYdR7kKKk09VrJNkEGJ0KQQ6imqR1Tn0kyu4cvkfqnCUF0rrn7CdBq
        LSCv1QRhgr6TChQf7ynWsPz5gGdVjh3tIw==
        =dsvF
        -----END PGP PUBLIC KEY BLOCK-----

packages:
- containerd
- [kubelet, {{.KubernetesVersion}}]
- [kubeadm, {{.KubernetesVersion}}]
`

	cloudInitConfigTemplate = `
datasource_list: [ None ]
datasource:
  None:
    metadata:
      local-hostname: %q
    userdata_raw: |
%s
`
)

func (ccp CloudConfig) validate() error {
	if ccp.BootstrapCloudConfig == "" {
		return fmt.Errorf("bootstrapCloudConfig can't be empty")
	}

	if ccp.KubernetesVersion == "" {
		return fmt.Errorf("kubernetesVersion can't be empty")
	}

	if ccp.ProviderID == "" {
		return fmt.Errorf("ProviderID can't be empty")
	}

	return nil
}

func (ccp CloudConfig) render() (string, error) {
	if err := ccp.validate(); err != nil {
		return "", fmt.Errorf("valdating config: %w", err)
	}

	tmpl := template.Must(template.New("cloud-config").Parse(cloudConfigTemplate))

	var buf bytes.Buffer

	if err := tmpl.Execute(&buf, ccp); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}

	// Set proper value for --provider-id kubelet flag.
	bootstrapCloudConfig := strings.ReplaceAll(ccp.BootstrapCloudConfig, providerIDPlaceholder, ccp.ProviderID)

	return bootstrapCloudConfig + buf.String(), nil
}

// indent indents a block of text with an indent string.
func indent(text, indent string) string {
	if text == "" {
		return ""
	}

	if text[len(text)-1:] == "\n" {
		result := ""
		for _, j := range strings.Split(text[:len(text)-1], "\n") {
			result += indent + j + "\n"
		}

		return result
	}

	result := ""

	for _, j := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		result += indent + j + "\n"
	}

	return result[:len(result)-1]
}
