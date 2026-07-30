/*
Copyright 2022 The Tinkerbell Authors.

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

package machine //nolint:testpackage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/scheme"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

func hardwareTestScheme(g Gomega) *runtime.Scheme {
	s := runtime.NewScheme()
	g.Expect(infrastructurev1.AddToScheme(s)).To(Succeed())

	sb := &scheme.Builder{GroupVersion: tinkv1.GroupVersion}
	sb.Register(&tinkv1.Hardware{}, &tinkv1.HardwareList{})
	g.Expect(sb.AddToScheme(s)).To(Succeed())

	return s
}

func newHardwareTestClient(g Gomega, objects ...client.Object) client.Client {
	s := hardwareTestScheme(g)

	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objects...).
		WithStatusSubresource(&tinkv1.Hardware{}).
		Build()
}

// blockingPatchClient pauses Patch calls until two claimers have reached the
// write boundary. This makes the read-modify-write race deterministic.
type blockingPatchClient struct {
	client.Client
	entered chan struct{}
	release chan struct{}
	patches atomic.Int32
	once    sync.Once
}

func (c *blockingPatchClient) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	if c.patches.Add(1) == 2 {
		c.once.Do(func() {
			close(c.entered)
		})
	}

	<-c.release
	if err := c.Client.Patch(ctx, obj, patch, opts...); err != nil {
		return fmt.Errorf("patching object after claim barrier: %w", err)
	}

	return nil
}

// Test_takeHardwareOwnership_owned_by_other_machine asserts that takeHardwareOwnership
// refuses to claim Hardware already labelled as owned by a different Machine, returning
// an error and leaving the object untouched (no resourceVersion bump).
func Test_takeHardwareOwnership_owned_by_other_machine(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	const (
		machineName      = "myMachine"
		machineNamespace = "myNamespace"
		hardwareName     = "myHardware"
	)

	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hardwareName,
			Namespace: machineNamespace,
			Labels: map[string]string{
				HardwareOwnerNameLabel:      "otherMachine",
				HardwareOwnerNamespaceLabel: "otherNamespace",
			},
		},
	}

	cl := newHardwareTestClient(g, hw)

	scope := &machineReconcileScope{
		ctx:               context.Background(),
		tinkerbellClient:  cl,
		tinkerbellMachine: &infrastructurev1.TinkerbellMachine{ObjectMeta: metav1.ObjectMeta{Name: machineName, Namespace: machineNamespace}},
	}
	before := &tinkv1.Hardware{}
	g.Expect(cl.Get(context.Background(), types.NamespacedName{Name: hardwareName, Namespace: machineNamespace}, before)).To(Succeed())

	err := scope.takeHardwareOwnership(hw)
	g.Expect(err).To(HaveOccurred(), "expected error claiming Hardware owned by a different Machine")
	g.Expect(errors.Is(err, errHardwareClaimRequeue)).To(BeTrue(),
		"expected ownership contention to request a quiet requeue")
	g.Expect(err.Error()).To(ContainSubstring("already owned"), "expected 'already owned' guard error")

	// The object must not have been written: a fresh Get yields the same resourceVersion.
	persisted := &tinkv1.Hardware{}
	g.Expect(cl.Get(context.Background(), types.NamespacedName{Name: hardwareName, Namespace: machineNamespace}, persisted)).To(Succeed())
	g.Expect(persisted.ResourceVersion).To(Equal(before.ResourceVersion),
		"Hardware must not be written when it already belongs to another Machine")
	g.Expect(persisted.Labels[HardwareOwnerNameLabel]).To(Equal("otherMachine"),
		"Hardware must keep its original owner label, not be claimed by this Machine")
}

// Test_takeHardwareOwnership_already_claimed_is_noop asserts that when Hardware is already
// claimed by this Machine with the expected finalizer state, takeHardwareOwnership is a
// no-op: it returns nil without bumping resourceVersion (avoids avoidable conflicts).
func Test_takeHardwareOwnership_already_claimed_is_noop(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	const (
		machineName      = "myMachine"
		machineNamespace = "myNamespace"
		hardwareName     = "myHardware"
	)

	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hardwareName,
			Namespace: machineNamespace,
			Labels: map[string]string{
				HardwareOwnerNameLabel:      machineName,
				HardwareOwnerNamespaceLabel: machineNamespace,
			},
		},
	}
	controllerutil.AddFinalizer(hw, infrastructurev1.MachineFinalizer)

	cl := newHardwareTestClient(g, hw)

	scope := &machineReconcileScope{
		ctx:               context.Background(),
		tinkerbellClient:  cl,
		tinkerbellMachine: &infrastructurev1.TinkerbellMachine{ObjectMeta: metav1.ObjectMeta{Name: machineName, Namespace: machineNamespace}},
	}

	before := &tinkv1.Hardware{}
	g.Expect(cl.Get(context.Background(), types.NamespacedName{Name: hardwareName, Namespace: machineNamespace}, before)).To(Succeed())

	g.Expect(scope.takeHardwareOwnership(hw)).To(Succeed(), "already-claimed Hardware should be a no-op, not an error")

	after := &tinkv1.Hardware{}
	g.Expect(cl.Get(context.Background(), types.NamespacedName{Name: hardwareName, Namespace: machineNamespace}, after)).To(Succeed())
	g.Expect(after.ResourceVersion).To(Equal(before.ResourceVersion),
		"already-claimed Hardware must not be rewritten on subsequent reconciles")
}

// Test_takeHardwareOwnership_concurrent_claims verifies that two Machines which
// read the same Hardware resourceVersion cannot both claim it. The barrier
// forces both stale objects to reach Patch before either write is allowed.
func Test_takeHardwareOwnership_concurrent_claims(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	const (
		hardwareName      = "myHardware"
		machineNamespace  = "myNamespace"
		firstMachineName  = "firstMachine"
		secondMachineName = "secondMachine"
	)

	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hardwareName,
			Namespace: machineNamespace,
		},
	}

	baseClient := newHardwareTestClient(g, hw)
	claimClient := &blockingPatchClient{
		Client:  baseClient,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}

	firstHardware := &tinkv1.Hardware{}
	secondHardware := &tinkv1.Hardware{}
	namespacedName := types.NamespacedName{Name: hardwareName, Namespace: machineNamespace}
	g.Expect(baseClient.Get(context.Background(), namespacedName, firstHardware)).To(Succeed())
	g.Expect(baseClient.Get(context.Background(), namespacedName, secondHardware)).To(Succeed())
	g.Expect(firstHardware.ResourceVersion).To(Equal(secondHardware.ResourceVersion))

	scope := func(machineName string) *machineReconcileScope {
		return &machineReconcileScope{
			ctx:              context.Background(),
			tinkerbellClient: claimClient,
			tinkerbellMachine: &infrastructurev1.TinkerbellMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      machineName,
					Namespace: machineNamespace,
				},
			},
		}
	}

	results := make(chan error, 2)
	go func() {
		results <- scope(firstMachineName).takeHardwareOwnership(firstHardware)
	}()
	go func() {
		results <- scope(secondMachineName).takeHardwareOwnership(secondHardware)
	}()

	select {
	case <-claimClient.entered:
	case <-time.After(time.Second):
		close(claimClient.release)
		t.Fatal("timed out waiting for both concurrent claims to reach Patch")
	}
	close(claimClient.release)

	firstErr := <-results
	secondErr := <-results
	errs := []error{firstErr, secondErr}

	successes := 0
	conflicts := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case apierrors.IsConflict(err):
			g.Expect(errors.Is(err, errHardwareClaimRequeue)).To(BeTrue(),
				"expected concurrent claim conflict to request a quiet requeue")
			conflicts++
		default:
			t.Fatalf("unexpected claim error: %v", err)
		}
	}

	g.Expect(successes).To(Equal(1), "exactly one Machine must win the Hardware claim")
	g.Expect(conflicts).To(Equal(1), "exactly one Machine must lose with a resourceVersion conflict")

	persisted := &tinkv1.Hardware{}
	g.Expect(baseClient.Get(context.Background(), namespacedName, persisted)).To(Succeed())
	g.Expect([]string{firstMachineName, secondMachineName}).To(
		ContainElement(persisted.Labels[HardwareOwnerNameLabel]),
		"the persisted owner must be one of the competing Machines",
	)
}
