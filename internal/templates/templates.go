/*
Copyright 2022 The Tinkerbell Authors.

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
	"fmt"
	"text/template"

	"github.com/pkg/errors"
)

var (
	// ErrMissingName is the error returned when the WorfklowTemplate Name is not specified.
	ErrMissingName = fmt.Errorf("name can't be empty")

	// ErrMissingImageURL is the error returned when the WorfklowTemplate ImageURL is not specified.
	ErrMissingImageURL = fmt.Errorf("imageURL can't be empty")
)

// WorkflowTemplate is a helper struct for rendering CAPT Template data.
type WorkflowTemplate struct {
	Name               string
	MetadataURL        string
	ImageURL           string
	DestDisk           string
	DestPartition      string
	DeviceTemplateName string
}

// Render renders workflow template for a given machine including user-data.
func (wt *WorkflowTemplate) Render() (string, error) {
	if wt.Name == "" {
		return "", ErrMissingName
	}

	if wt.ImageURL == "" {
		return "", ErrMissingImageURL
	}

	if wt.DeviceTemplateName == "" {
		wt.DeviceTemplateName = "{{.device_1}}"
	}

	tpl, err := template.New("template").Parse(workflowTemplate)
	if err != nil {
		return "", errors.Wrap(err, "unable to parse template")
	}

	buf := &bytes.Buffer{}

	err = tpl.Execute(buf, wt)
	if err != nil {
		return "", errors.Wrap(err, "unable to execute template")
	}

	return buf.String(), nil
}

const (
	workflowTemplate = `
version: "0.1"
name: {{.Name}}
global_timeout: 6000
tasks:
  - name: "{{.Name}}"
    worker: "{{.DeviceTemplateName}}"
    volumes:
      - /dev:/dev
      - /dev/console:/dev/console
      - /lib/firmware:/lib/firmware:ro
    actions:
      - name: "stream-image"
        image: oci2disk:v1.0.0
        timeout: 600
        environment:
          IMG_URL: {{.ImageURL}}
          DEST_DISK: {{.DestDisk}}
          COMPRESSED: true
      - name: "add-tink-cloud-init-config"
        image: writefile:v1.0.0
        timeout: 90
        environment:
          DEST_DISK: {{.DestPartition}}
          FS_TYPE: ext4
          DEST_PATH: /etc/cloud/cloud.cfg.d/10_tinkerbell.cfg
          UID: 0
          GID: 0
          MODE: 0600
          DIRMODE: 0700
          CONTENTS: |
            datasource:
              Ec2:
                metadata_urls: ["{{.MetadataURL}}"]
                strict_id: false
            system_info:
              default_user:
                name: tink
                groups: [wheel, adm]
                sudo: ["ALL=(ALL) NOPASSWD:ALL"]
                shell: /bin/bash
            manage_etc_hosts: localhost
            warnings:
              dsid_missing_source: off
      - name: "add-tink-cloud-init-ds-config"
        image: writefile:v1.0.0
        timeout: 90
        environment:
          DEST_DISK: {{.DestPartition}}
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
          BLOCK_DEVICE: {{.DestPartition}}
          FS_TYPE: ext4
`
)
