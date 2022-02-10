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
	golang.org/x/net v0.0.0-20211209124913-491a49abca63 // indirect
	google.golang.org/grpc v1.42.0
	google.golang.org/protobuf v1.27.1
	k8s.io/api v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/client-go v0.23.0
	k8s.io/component-base v0.23.0
	k8s.io/klog/v2 v2.30.0
	k8s.io/utils v0.0.0-20210930125809-cb0fa318a74b
	sigs.k8s.io/cluster-api v1.1.1
	sigs.k8s.io/controller-runtime v0.11.0
	sigs.k8s.io/structured-merge-diff/v4 v4.2.1 // indirect
	sigs.k8s.io/yaml v1.3.0
)
