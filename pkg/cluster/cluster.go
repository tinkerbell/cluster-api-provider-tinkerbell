// Package cluster builds a controller-runtime cluster client for interacting with objects.
// This client can be local in-cluster or remote, depending on the provided configuration.
package cluster

import (
	"fmt"
	"os"
	"path/filepath"

	rufiov1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// NewClient creates a controller-runtime cluster.Cluster interface.
func NewClient(rc *rest.Config, clusterOpts ...cluster.Option) (cluster.Cluster, error) {
	c, err := cluster.New(rc, clusterOpts...)
	if err != nil {
		return nil, fmt.Errorf("creating cluster for remote Tinkerbell: %w", err)
	}

	return c, nil
}

// DefaultOption returns a cluster.Option that configures the cluster's cache to informer-watch Workflow and Job objects.
func DefaultOption(rs *runtime.Scheme, namespace string) cluster.Option {
	opt := func(o *cluster.Options) {
		o.Scheme = rs
		o.Cache.ByObject = map[client.Object]cache.ByObject{
			&tinkv1.Workflow{}: {},
			&rufiov1.Job{}:     {},
		}
		if namespace != "" {
			o.Cache.DefaultNamespaces = map[string]cache.Config{namespace: {}}
		}
	}

	return opt
}

// RestConfig returns a Kubernetes REST client configuration for connecting to the Tinkerbell cluster.
// It attempts to read kubeconfig data from the specified file location.
// If the file is absent or empty, it returns a NoConfigError.
func RestConfig(kubeconfigLocation string) (*rest.Config, error) {
	if data, err := os.ReadFile(filepath.Clean(kubeconfigLocation)); err == nil && len(data) > 0 {
		cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
		}

		return cfg, nil
	}

	return nil, NoConfigError{}
}

// NoConfigError is a custom error for no kubeconfig data provided for Tinkerbell cluster.
type NoConfigError struct{}

func (e NoConfigError) Error() string {
	return "no kubeconfig data provided for Tinkerbell cluster"
}
