// Package cluster builds a controller-runtime cluster client for interacting with objects.
// This client can be local or external, depending on the provided configuration.
package cluster

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RestConfig returns a Kubernetes REST client configuration for connecting to the Tinkerbell cluster.
// It attempts to read kubeconfig data from the specified file location.
// If the file is absent or empty, it returns a NoConfigError.
func RestConfig(kubeconfigLocation string) (*rest.Config, error) {
	if kubeconfigLocation == "" {
		return nil, NoConfigError{}
	}

	data, err := os.ReadFile(filepath.Clean(kubeconfigLocation))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NoConfigError{}
		}

		return nil, fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	if len(data) == 0 {
		return nil, NoConfigError{}
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	return cfg, nil
}

// NoConfigError is a custom error for no kubeconfig data provided for Tinkerbell cluster.
type NoConfigError struct{}

func (e NoConfigError) Error() string {
	return "no kubeconfig data provided for Tinkerbell cluster"
}

// Is reports whether target is a NoConfigError.
func (NoConfigError) Is(target error) bool {
	_, ok := target.(NoConfigError)
	return ok
}

// NewDirectClient creates a non-cached controller-runtime client for direct
// API server calls. Used when cluster-wide informer watches are not available.
func NewDirectClient(cfg *rest.Config, rs *runtime.Scheme) (client.Client, error) {
	c, err := client.New(cfg, client.Options{Scheme: rs})
	if err != nil {
		return nil, fmt.Errorf("creating direct client for external Tinkerbell: %w", err)
	}

	return c, nil
}
