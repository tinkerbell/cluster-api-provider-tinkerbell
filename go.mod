module github.com/tinkerbell/cluster-api-provider-tinkerbell

go 1.15

require (
	github.com/go-logr/logr v0.4.0
	github.com/google/uuid v1.3.0
	github.com/onsi/gomega v1.16.0
	github.com/tinkerbell/tink v0.0.0-20210104124527-57eb0efb6dbb
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad // indirect
	google.golang.org/genproto v0.0.0-20201214200347-8c77b98c765d // indirect
	google.golang.org/grpc v1.40.0
	google.golang.org/protobuf v1.26.0
	k8s.io/api v0.17.16
	k8s.io/apimachinery v0.17.16
	k8s.io/client-go v0.17.16
	k8s.io/klog v1.0.0
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	sigs.k8s.io/cluster-api v0.3.15
	sigs.k8s.io/controller-runtime v0.5.14
	sigs.k8s.io/yaml v1.2.0
)
