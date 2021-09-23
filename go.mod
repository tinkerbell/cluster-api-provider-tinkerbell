module github.com/tinkerbell/cluster-api-provider-tinkerbell

go 1.16

require (
	github.com/go-logr/logr v0.4.0
	github.com/google/uuid v1.3.0
	github.com/onsi/gomega v1.16.0
	github.com/prometheus/common v0.30.0 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/tinkerbell/tink v0.0.0-20210910200746-3743d31e0cf0
	go.uber.org/atomic v1.9.0 // indirect
	golang.org/x/net v0.0.0-20210825183410-e898025ed96a // indirect
	golang.org/x/oauth2 v0.0.0-20210819190943-2bc19b11175f // indirect
	google.golang.org/grpc v1.40.0
	google.golang.org/protobuf v1.27.1
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/component-base v0.22.1
	k8s.io/klog/v2 v2.10.0
	k8s.io/utils v0.0.0-20210802155522-efc7438f0176
	sigs.k8s.io/cluster-api v0.4.3
	sigs.k8s.io/controller-runtime v0.9.7
	sigs.k8s.io/yaml v1.3.0
)
