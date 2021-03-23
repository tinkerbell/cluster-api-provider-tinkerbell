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
	"fmt"
	"net/url"
	"path"
	"strings"
)

// WorkflowTemplate is a helper struct for rendering CAPT Template data.
type WorkflowTemplate struct {
	Name              string
	ImageSourceURL    string
	KubernetesVersion string
	DestDisk          string
	DestPartition     string
}

// Render renders workflow template for a given machine including user-data.
func (wt WorkflowTemplate) Render() (string, error) {
	if wt.Name == "" {
		return "", fmt.Errorf("name can't be empty")
	}

	url, err := url.Parse(wt.ImageSourceURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse url: %w", err)
	}

	imageName := fmt.Sprintf("ubuntu-1804-kube-%s.gz", wt.KubernetesVersion)
	url.Path = path.Join(url.Path, imageName)
	imageURL := url.String()

	return fmt.Sprintf(workflowTemplate, wt.Name, wt.Name, imageURL, wt.DestDisk, wt.DestPartition, wt.DestPartition, wt.DestPartition), nil
}

const (
	workflowTemplate = `
version: "0.1"
name: %s
global_timeout: 6000
tasks:
  - name: "%s"
    worker: "{{.device_1}}"
    volumes:
      - /dev:/dev
      - /dev/console:/dev/console
      - /lib/firmware:/lib/firmware:ro
    actions:
      - name: "stream-image"
        image: image2disk:v1.0.0
        timeout: 360
        environment:
          IMG_URL: %s
          DEST_DISK: %s
          COMPRESSED: true
      - name: "add-tink-cloud-init-config"
        image: writefile:v1.0.0
        timeout: 90
        environment:
          DEST_DISK: %s
          FS_TYPE: ext4
          DEST_PATH: /etc/cloud/cloud.cfg.d/10_tinkerbell.cfg
          UID: 0
          GID: 0
          MODE: 0600
          DIRMODE: 0700
          CONTENTS: |
            datasource:
              Ec2:
                metadata_urls: ["http://169.254.169.254:50061"]
                strict_id: false
            system_info:
              default_user:
                name: tink
                groups: [wheel, adm]
                sudo: ["ALL=(ALL) NOPASSWD:ALL"]
                shell: /bin/bash
            warnings:
              dsid_missing_source: off
      - name: "add-tink-cloud-init-ds-config"
        image: writefile:v1.0.0
        timeout: 90
        environment:
          DEST_DISK: %s
          FS_TYPE: ext4
          DEST_PATH: /etc/cloud/ds-identify.cfg
          UID: 0
          GID: 0
          MODE: 0600
          DIRMODE: 0700
          CONTENTS: |
            datasource: Ec2
      - name: "kexec-image"
        image: kexec:v1.0.0
        timeout: 90
        pid: host
        environment:
          BLOCK_DEVICE: %s
          FS_TYPE: ext4
`
)

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
