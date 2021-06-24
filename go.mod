module github.com/tinkerbell/cluster-api-provider-tinkerbell

go 1.15

require (
	github.com/go-logr/logr v0.4.0
	github.com/google/uuid v1.2.0
	github.com/onsi/gomega v1.13.0
	github.com/tinkerbell/tink v0.0.0-20210104124527-57eb0efb6dbb
	google.golang.org/grpc v1.38.0
	google.golang.org/protobuf v1.26.0
	k8s.io/api v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/klog v1.0.0
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	sigs.k8s.io/cluster-api v0.4.0
	sigs.k8s.io/controller-runtime v0.9.1
	sigs.k8s.io/yaml v1.2.0
)
