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

package templates

import (
	"testing"
)

func Test_Indent(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input       string
		indentation string
		result      string
	}{
		"adds_given_indent_to_string_without_trailing_whitespace": {
			input:       "foo",
			indentation: "   ",
			result:      "   foo",
		},
		"adds_given_indent_to_every_given_line_of_text": {
			input:       "foo\nbar\n",
			indentation: "  ",
			result:      "  foo\n  bar\n",
		},
		"does_not_add_any_indentation_to_an_empty_text_when_indentation_is_empty": {
			input:       "",
			indentation: "",
			result:      "",
		},
		"does_not_add_any_indentation_to_an_empty_text": {
			input:       "",
			indentation: "  ",
			result:      "",
		},
	}

	for caseName, c := range cases { //nolint:paralleltest
		c := c

		t.Run(caseName, func(t *testing.T) {
			t.Parallel()

			if result := indent(c.input, c.indentation); result != c.result {
				t.Fatalf("expected %q, got %q", c.result, result)
			}
		})
	}
}
