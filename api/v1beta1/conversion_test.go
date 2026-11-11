/*
Copyright The Tinkerbell Authors.

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

package v1beta1

import (
	"math/rand"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/randfill"

	infrav2 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta2"
)

// cmpOpts are comparison options shared across all round-trip tests.
// EquateEmpty treats nil and empty slices/maps as equal.
var cmpOpts = cmp.Options{cmpopts.EquateEmpty()}

// cleanMeta removes Annotations (used by MarshalData/UnmarshalData for
// round-trip stashing) and ManagedFields (contains FieldsV1 CBOR that can't
// survive JSON round-trip) before fuzzing so they don't interfere.
func cleanMeta(obj metav1.Object) {
	obj.SetAnnotations(nil)
	obj.SetManagedFields(nil)
}

// v1beta1FuzzerFuncs zeroes out v1beta1-only deprecated fields that cannot
// survive a spoke→hub→spoke round-trip because they have no v1beta2 equivalent.
func v1beta1FuzzerFuncs(_ runtimeserializer.CodecFactory) []interface{} {
	return []interface{}{
		func(spec *TinkerbellClusterSpec, c randfill.Continue) {
			c.FillNoCustom(spec)
			spec.ImageLookupFormat = ""
			spec.ImageLookupBaseRegistry = ""
			spec.ImageLookupOSDistro = ""
			spec.ImageLookupOSVersion = ""
		},
		func(spec *TinkerbellMachineSpec, c randfill.Continue) {
			c.FillNoCustom(spec)
			spec.ImageLookupFormat = ""
			spec.ImageLookupBaseRegistry = ""
			spec.ImageLookupOSDistro = ""
			spec.ImageLookupOSVersion = ""
		},
		func(res *TinkerbellMachineTemplateResource, c randfill.Continue) {
			c.FillNoCustom(res)
			res.Spec.ImageLookupFormat = ""
			res.Spec.ImageLookupBaseRegistry = ""
			res.Spec.ImageLookupOSDistro = ""
			res.Spec.ImageLookupOSVersion = ""
			res.Spec.HardwareName = ""
			res.Spec.ProviderID = ""
		},
	}
}

func TestFuzzyConversion(t *testing.T) {
	t.Parallel()
	f := fuzzer.FuzzerFor(fuzzer.MergeFuzzerFuncs(v1beta1FuzzerFuncs), rand.NewSource(0), runtimeserializer.CodecFactory{})

	t.Run("TinkerbellCluster spoke-hub-spoke", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			spoke := &TinkerbellCluster{}
			f.Fill(spoke)
			spoke.TypeMeta = metav1.TypeMeta{}
			cleanMeta(spoke)

			hub := &infrav2.TinkerbellCluster{}
			if err := ConvertClusterToHub(spoke, hub); err != nil {
				t.Fatalf("ConvertClusterToHub: %v", err)
			}
			result := &TinkerbellCluster{}
			if err := ConvertClusterFromHub(result, hub); err != nil {
				t.Fatalf("ConvertClusterFromHub: %v", err)
			}
			delete(result.Annotations, dataAnnotation)
			if len(result.Annotations) == 0 {
				result.Annotations = nil
			}
			if diff := cmp.Diff(spoke, result, cmpOpts); diff != "" {
				t.Errorf("spoke-hub-spoke round-trip mismatch (-want +got):\n%s", diff)
			}
		}
	})

	t.Run("TinkerbellCluster hub-spoke-hub", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			hub := &infrav2.TinkerbellCluster{}
			f.Fill(hub)
			hub.TypeMeta = metav1.TypeMeta{}
			cleanMeta(hub)

			spoke := &TinkerbellCluster{}
			if err := ConvertClusterFromHub(spoke, hub); err != nil {
				t.Fatalf("ConvertClusterFromHub: %v", err)
			}
			result := &infrav2.TinkerbellCluster{}
			if err := ConvertClusterToHub(spoke, result); err != nil {
				t.Fatalf("ConvertClusterToHub: %v", err)
			}
			delete(result.Annotations, dataAnnotation)
			if len(result.Annotations) == 0 {
				result.Annotations = nil
			}
			if diff := cmp.Diff(hub, result, cmpOpts); diff != "" {
				t.Errorf("hub-spoke-hub round-trip mismatch (-want +got):\n%s", diff)
			}
		}
	})

	t.Run("TinkerbellMachine spoke-hub-spoke", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			spoke := &TinkerbellMachine{}
			f.Fill(spoke)
			spoke.TypeMeta = metav1.TypeMeta{}
			cleanMeta(spoke)

			hub := &infrav2.TinkerbellMachine{}
			if err := ConvertMachineToHub(spoke, hub); err != nil {
				t.Fatalf("ConvertMachineToHub: %v", err)
			}
			result := &TinkerbellMachine{}
			if err := ConvertMachineFromHub(result, hub); err != nil {
				t.Fatalf("ConvertMachineFromHub: %v", err)
			}
			delete(result.Annotations, dataAnnotation)
			if len(result.Annotations) == 0 {
				result.Annotations = nil
			}
			if diff := cmp.Diff(spoke, result, cmpOpts); diff != "" {
				t.Errorf("spoke-hub-spoke round-trip mismatch (-want +got):\n%s", diff)
			}
		}
	})

	t.Run("TinkerbellMachine hub-spoke-hub", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			hub := &infrav2.TinkerbellMachine{}
			f.Fill(hub)
			hub.TypeMeta = metav1.TypeMeta{}
			cleanMeta(hub)

			spoke := &TinkerbellMachine{}
			if err := ConvertMachineFromHub(spoke, hub); err != nil {
				t.Fatalf("ConvertMachineFromHub: %v", err)
			}
			result := &infrav2.TinkerbellMachine{}
			if err := ConvertMachineToHub(spoke, result); err != nil {
				t.Fatalf("ConvertMachineToHub: %v", err)
			}
			delete(result.Annotations, dataAnnotation)
			if len(result.Annotations) == 0 {
				result.Annotations = nil
			}
			if diff := cmp.Diff(hub, result, cmpOpts); diff != "" {
				t.Errorf("hub-spoke-hub round-trip mismatch (-want +got):\n%s", diff)
			}
		}
	})

	t.Run("TinkerbellMachineTemplate spoke-hub-spoke", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			spoke := &TinkerbellMachineTemplate{}
			f.Fill(spoke)
			spoke.TypeMeta = metav1.TypeMeta{}
			cleanMeta(spoke)

			hub := &infrav2.TinkerbellMachineTemplate{}
			if err := ConvertMachineTemplateToHub(spoke, hub); err != nil {
				t.Fatalf("ConvertMachineTemplateToHub: %v", err)
			}
			result := &TinkerbellMachineTemplate{}
			if err := ConvertMachineTemplateFromHub(result, hub); err != nil {
				t.Fatalf("ConvertMachineTemplateFromHub: %v", err)
			}
			delete(result.Annotations, dataAnnotation)
			if len(result.Annotations) == 0 {
				result.Annotations = nil
			}
			if diff := cmp.Diff(spoke, result, cmpOpts); diff != "" {
				t.Errorf("spoke-hub-spoke round-trip mismatch (-want +got):\n%s", diff)
			}
		}
	})

	t.Run("TinkerbellMachineTemplate hub-spoke-hub", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			hub := &infrav2.TinkerbellMachineTemplate{}
			f.Fill(hub)
			hub.TypeMeta = metav1.TypeMeta{}
			cleanMeta(hub)

			spoke := &TinkerbellMachineTemplate{}
			if err := ConvertMachineTemplateFromHub(spoke, hub); err != nil {
				t.Fatalf("ConvertMachineTemplateFromHub: %v", err)
			}
			result := &infrav2.TinkerbellMachineTemplate{}
			if err := ConvertMachineTemplateToHub(spoke, result); err != nil {
				t.Fatalf("ConvertMachineTemplateToHub: %v", err)
			}
			delete(result.Annotations, dataAnnotation)
			if len(result.Annotations) == 0 {
				result.Annotations = nil
			}
			if diff := cmp.Diff(hub, result, cmpOpts); diff != "" {
				t.Errorf("hub-spoke-hub round-trip mismatch (-want +got):\n%s", diff)
			}
		}
	})
}
