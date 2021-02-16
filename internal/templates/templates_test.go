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

package templates_test

import (
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/internal/templates"
)

func validWorkflowTemplate() *templates.WorkflowTemplate {
	return &templates.WorkflowTemplate{
		Name: "foo",
		CloudInitConfig: templates.CloudInitConfig{
			Hostname: "foo",
			CloudConfig: templates.CloudConfig{
				BootstrapCloudConfig: "foo: bar",
				KubernetesVersion:    "bar",
				ProviderID:           "bar",
			},
		},
	}
}

//nolint:funlen
func Test_Cloud_config_template(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutateF     func(*templates.WorkflowTemplate)
		expectError bool
		validateF   func(*testing.T, *templates.WorkflowTemplate, string)
	}{
		"requires_non_empty_bootstrap_cloud_config": {
			mutateF: func(wt *templates.WorkflowTemplate) {
				wt.CloudInitConfig.CloudConfig.BootstrapCloudConfig = ""
			},
			expectError: true,
		},

		"requires_non_empty_Kubernetes_version": {
			mutateF: func(wt *templates.WorkflowTemplate) {
				wt.CloudInitConfig.CloudConfig.KubernetesVersion = ""
			},
			expectError: true,
		},

		"requires_non_empty_provider_ID": {
			mutateF: func(wt *templates.WorkflowTemplate) {
				wt.CloudInitConfig.CloudConfig.ProviderID = ""
			},
			expectError: true,
		},

		"renders_with_valid_config": {
			mutateF: func(wt *templates.WorkflowTemplate) {},
		},

		// This is to avoid malforming bootstrap cloud config if it includes some template syntax, as we do not
		// have control over it.
		"rendering_does_not_template_bootstrap_cloud_config": {
			mutateF: func(wt *templates.WorkflowTemplate) {
				wt.CloudInitConfig.CloudConfig.BootstrapCloudConfig = "{{.foo}"
			},
		},

		"rendered_output_should_be_valid_YAML": {
			validateF: func(t *testing.T, wt *templates.WorkflowTemplate, renderResult string) { //nolint:thelper
				x := &map[string]interface{}{}

				if err := yaml.Unmarshal([]byte(renderResult), x); err != nil {
					t.Fatalf("Should unmarshal successfully, got: %v", err)
				}
			},
		},
	}

	for name, c := range cases { //nolint:paralleltest
		c := c

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wt := validWorkflowTemplate()

			if c.mutateF != nil {
				c.mutateF(wt)
			}

			result, err := wt.Render()

			if c.expectError && err == nil {
				t.Fatalf("Expected error")
			}

			if !c.expectError && err != nil {
				t.Fatalf("Did not expect error, got: %v", err)
			}

			if c.validateF == nil {
				return
			}

			c.validateF(t, wt, result)
		})
	}
}
