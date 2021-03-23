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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

const (
	// ClusterFinalizer allows ReconcileTinkerbellCluster to clean up Tinkerbell resources before
	// removing it from the apiserver.
	ClusterFinalizer = "tinkerbellcluster.infrastructure.cluster.x-k8s.io"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TinkerbellClusterSpec defines the desired state of TinkerbellCluster
// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
// Important: Run "make" to regenerate code after modifying this file.
type TinkerbellClusterSpec struct {
	// ImageBaseURL is the base URL that is used for pulling images, if not set, the default
	// will be to use http://<TINKERBELL IP>:8080/
	//
	// +optional
	ImageBaseURL string `json:"imageBaseURL,omitempty"`

	// ControlPlaneEndpoint is a required field by ClusterAPI v1alpha3.
	//
	// See https://cluster-api.sigs.k8s.io/developer/architecture/controllers/cluster.html
	// for more details.
	//
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`
}

// TinkerbellClusterStatus defines the observed state of TinkerbellCluster.
type TinkerbellClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file.

	// Ready denotes that the cluster (infrastructure) is ready.
	// +optional
	Ready bool `json:"ready"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true

// TinkerbellCluster is the Schema for the tinkerbellclusters API.
type TinkerbellCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TinkerbellClusterSpec   `json:"spec,omitempty"`
	Status TinkerbellClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TinkerbellClusterList contains a list of TinkerbellCluster.
type TinkerbellClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TinkerbellCluster `json:"items"`
}

//nolint:gochecknoinits
func init() {
	SchemeBuilder.Register(&TinkerbellCluster{}, &TinkerbellClusterList{})
}
