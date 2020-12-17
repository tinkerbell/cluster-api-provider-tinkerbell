module github.com/tinkerbell/cluster-api-provider-tinkerbell

go 1.15

require (
	github.com/go-logr/logr v0.1.0
	github.com/google/uuid v1.1.2
	github.com/onsi/gomega v1.10.1
	github.com/tinkerbell/tink v0.0.0-20201210163923-6d9159b63857
	google.golang.org/grpc v1.32.0
	google.golang.org/protobuf v1.25.0
	k8s.io/api v0.17.16
	k8s.io/apimachinery v0.17.16
	k8s.io/client-go v0.17.16
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	sigs.k8s.io/cluster-api v0.3.12
	sigs.k8s.io/controller-runtime v0.5.14
)
