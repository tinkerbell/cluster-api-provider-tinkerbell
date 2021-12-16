module github.com/tinkerbell/cluster-api-provider-tinkerbell

go 1.16

require (
	github.com/go-logr/logr v1.2.0
	github.com/google/uuid v1.3.0
	github.com/onsi/gomega v1.17.0
	github.com/prometheus/common v0.30.0 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/tinkerbell/tink v0.0.0-20210910200746-3743d31e0cf0
	go.uber.org/atomic v1.9.0 // indirect
	google.golang.org/grpc v1.41.0
	google.golang.org/protobuf v1.27.1
	k8s.io/api v0.23.1
	k8s.io/apimachinery v0.23.1
	k8s.io/client-go v0.23.1
	k8s.io/component-base v0.23.1
	k8s.io/klog/v2 v2.30.0
	k8s.io/utils v0.0.0-20210930125809-cb0fa318a74b
	sigs.k8s.io/cluster-api v1.0.2
	sigs.k8s.io/controller-runtime v0.10.3
	sigs.k8s.io/yaml v1.3.0
)
