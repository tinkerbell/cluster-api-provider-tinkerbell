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

package v1beta1_test

import (
	"context"
	"testing"

	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

func Test_valid_tinkerbell_cluster(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	existing := &v1beta1.TinkerbellCluster{}

	for _, cluster := range []v1beta1.TinkerbellCluster{
		// only templateOverride set
		{
			Spec: v1beta1.TinkerbellClusterSpec{
				TemplateOverride: "some-template-data",
			},
		},
		// only templateOverrideRef set
		{
			Spec: v1beta1.TinkerbellClusterSpec{
				TemplateOverrideRef: &v1beta1.ObjectRef{
					Name:      "my-template",
					Namespace: "default",
				},
			},
		},
		// neither set
		{
			Spec: v1beta1.TinkerbellClusterSpec{},
		},
	} {
		_, err := cluster.ValidateCreate(context.Background(), nil)
		g.Expect(err).ToNot(HaveOccurred())
		_, err = cluster.ValidateUpdate(context.Background(), existing, &cluster)
		g.Expect(err).ToNot(HaveOccurred())
	}
}

func Test_invalid_tinkerbell_cluster(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	existing := &v1beta1.TinkerbellCluster{}

	// both templateOverride and templateOverrideRef set
	cluster := v1beta1.TinkerbellCluster{
		Spec: v1beta1.TinkerbellClusterSpec{
			TemplateOverride: "some-template-data",
			TemplateOverrideRef: &v1beta1.ObjectRef{
				Name:      "my-template",
				Namespace: "default",
			},
		},
	}

	_, err := cluster.ValidateCreate(context.Background(), nil)
	g.Expect(err).To(HaveOccurred())
	_, err = cluster.ValidateUpdate(context.Background(), existing, &cluster)
	g.Expect(err).To(HaveOccurred())
}
