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

package templates_test

import (
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"github.com/tinkerbell/cluster-api-provider-tinkerbell/internal/templates"
)

func validWorkflowTemplate() *templates.WorkflowTemplate {
	return &templates.WorkflowTemplate{
		Name:          "foo",
		MetadataURL:   "http://10.10.10.10",
		ImageURL:      "http://foo.bar.baz/do/it",
		DestDisk:      "/dev/sda",
		DestPartition: "/dev/sda1",
	}
}

//nolint:funlen
func Test_Cloud_config_template(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutateF       func(*templates.WorkflowTemplate)
		expectError   bool
		expectedError error
		validateF     func(*testing.T, *templates.WorkflowTemplate, string)
	}{
		"requires_non_empty_ImageURL": {
			mutateF: func(wt *templates.WorkflowTemplate) {
				wt.ImageURL = ""
			},
			expectError:   true,
			expectedError: templates.ErrMissingImageURL,
		},

		"requires_non_empty_Name": {
			mutateF: func(wt *templates.WorkflowTemplate) {
				wt.Name = ""
			},
			expectError:   true,
			expectedError: templates.ErrMissingName,
		},

		"renders_with_valid_config": {
			mutateF: func(wt *templates.WorkflowTemplate) {},
		},

		"rendered_output_should_be_valid_YAML": {
			validateF: func(t *testing.T, wt *templates.WorkflowTemplate, renderResult string) { //nolint:thelper
				g := NewWithT(t)
				x := &map[string]interface{}{}

				g.Expect(yaml.Unmarshal([]byte(renderResult), x)).To(Succeed())
			},
		},
	}

	// nolint:paralleltest
	for name, c := range cases {
		c := c

		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			wt := validWorkflowTemplate()

			if c.mutateF != nil {
				c.mutateF(wt)
			}

			result, err := wt.Render()

			if c.expectError {
				if c.expectedError != nil {
					g.Expect(err).To(MatchError(c.expectedError))
				} else {
					g.Expect(err).To(HaveOccurred())
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			if c.validateF == nil {
				return
			}

			c.validateF(t, wt, result)
		})
	}
}
