/*
Copyright 2026 The Tinkerbell Authors.

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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega" //nolint:revive // one day we will remove gomega
	rufiov1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/bmc"
	tinkv1 "github.com/tinkerbell/tinkerbell/api/v1alpha1/tinkerbell"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
)

const (
	bmcTestMachineName         = "my-machine"
	bmcTestMachineNamespace    = "mgmt-ns"
	bmcTestTinkerbellNamespace = "tink-ns"
	bmcTestHardwareName        = "my-hardware"
	bmcTestBMCName             = "my-bmc"
)

func newBMCTestClient(g Gomega, funcs interceptor.Funcs, objects ...client.Object) client.Client {
	s := runtime.NewScheme()
	g.Expect(infrastructurev1.AddToScheme(s)).To(Succeed())
	g.Expect(tinkv1.AddToScheme(s)).To(Succeed())
	g.Expect(rufiov1.AddToScheme(s)).To(Succeed())

	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objects...).
		WithStatusSubresource(&infrastructurev1.TinkerbellMachine{}, &tinkv1.Hardware{}).
		WithInterceptorFuncs(funcs).
		Build()
}

func setHardwareOwner(hw *tinkv1.Hardware, name, namespace string) {
	hw.Labels = map[string]string{
		HardwareOwnerNameLabel:      name,
		HardwareOwnerNamespaceLabel: namespace,
	}
}

// claimedHardware returns Hardware in the Tinkerbell namespace, claimed by the test
// TinkerbellMachine in machineNamespace and with a BMC reference, so deleting the machine
// powers it off.
func claimedHardware(machineNamespace string) *tinkv1.Hardware {
	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bmcTestHardwareName,
			Namespace:  bmcTestTinkerbellNamespace,
			Finalizers: []string{infrastructurev1.MachineFinalizer},
		},
		Spec: tinkv1.HardwareSpec{
			BMCRef: &corev1.TypedLocalObjectReference{Name: bmcTestBMCName},
		},
	}
	setHardwareOwner(hw, bmcTestMachineName, machineNamespace)

	return hw
}

// legacyPowerOffJob returns a completed power off Job named the way earlier versions named it,
// labeled for the TinkerbellMachine machineName in machineNamespace.
func legacyPowerOffJob(machineName, machineNamespace string) *rufiov1.Job {
	job := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmcTestMachineName + "-poweroff",
			Namespace: bmcTestTinkerbellNamespace,
			Labels: map[string]string{
				LabelMachineName:      machineName,
				LabelMachineNamespace: machineNamespace,
			},
		},
	}
	job.SetCondition(rufiov1.JobCompleted, rufiov1.ConditionTrue)

	return job
}

// deletingMachineScope creates a TinkerbellMachine with the given UID in machineNamespace, whose
// Hardware is in the Tinkerbell namespace, deletes it, and returns a scope for its delete reconcile.
func deletingMachineScope(g Gomega, c client.Client, machineNamespace string, uid types.UID) *machineReconcileScope {
	ctx := context.Background()

	tm := &infrastructurev1.TinkerbellMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       bmcTestMachineName,
			Namespace:  machineNamespace,
			UID:        uid,
			Finalizers: []string{infrastructurev1.MachineFinalizer},
		},
		Spec: infrastructurev1.TinkerbellMachineSpec{
			HardwareName: bmcTestHardwareName,
		},
	}
	g.Expect(c.Create(ctx, tm)).To(Succeed())

	tm.Status.TargetNamespace = bmcTestTinkerbellNamespace
	g.Expect(c.Status().Update(ctx, tm)).To(Succeed())
	g.Expect(c.Delete(ctx, tm)).To(Succeed())

	return reloadMachineScope(g, c, machineNamespace)
}

// reloadMachineScope returns a scope for the next reconcile of the test TinkerbellMachine.
func reloadMachineScope(g Gomega, c client.Client, machineNamespace string) *machineReconcileScope {
	ctx := context.Background()

	tm := &infrastructurev1.TinkerbellMachine{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: bmcTestMachineName, Namespace: machineNamespace}, tm)).To(Succeed())

	patchHelper, err := patch.NewHelper(tm, c)
	g.Expect(err).NotTo(HaveOccurred())

	return &machineReconcileScope{
		log:               logr.Discard(),
		ctx:               ctx,
		tinkerbellMachine: tm,
		patchHelper:       patchHelper,
		client:            c,
		tinkerbellClient:  c,
		tinkerbellReader:  c,
		scheme:            c.Scheme(),
	}
}

func getJob(c client.Client, name string) (*rufiov1.Job, error) {
	job := &rufiov1.Job{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: bmcTestTinkerbellNamespace}, job); err != nil {
		return nil, fmt.Errorf("getting Job %s: %w", name, err)
	}

	return job, nil
}

func completeJob(g Gomega, c client.Client, name string) {
	job, err := getJob(c, name)
	g.Expect(err).NotTo(HaveOccurred())
	job.SetCondition(rufiov1.JobCompleted, rufiov1.ConditionTrue)
	g.Expect(c.Update(context.Background(), job)).To(Succeed())
}

func expectJobGone(g Gomega, c client.Client, name string) {
	_, err := getJob(c, name)
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected Job %s to be deleted, got %v", name, err)
}

func getHardware(g Gomega, c client.Client) *tinkv1.Hardware {
	hw := &tinkv1.Hardware{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Name: bmcTestHardwareName, Namespace: bmcTestTinkerbellNamespace}, hw)).To(Succeed())

	return hw
}

func expectMachineGone(g Gomega, c client.Client, machineNamespace string) {
	err := c.Get(context.Background(), types.NamespacedName{Name: bmcTestMachineName, Namespace: machineNamespace}, &infrastructurev1.TinkerbellMachine{})
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected TinkerbellMachine to be deleted, got %v", err)
}

// expectPowerOffPending checks that a delete reconcile created the named power off Job and
// waits for it, holding the Hardware and the TinkerbellMachine finalizer.
func expectPowerOffPending(g Gomega, c client.Client, machineNamespace, jobName string) *rufiov1.Job {
	job, err := getJob(c, jobName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(job.HasCondition(rufiov1.JobCompleted, rufiov1.ConditionTrue)).To(BeFalse())
	g.Expect(job.Spec.MachineRef).To(Equal(rufiov1.MachineRef{Name: bmcTestBMCName, Namespace: bmcTestTinkerbellNamespace}))
	g.Expect(job.Spec.Tasks).To(HaveLen(1))
	g.Expect(job.Spec.Tasks[0].PowerAction).To(HaveValue(Equal(rufiov1.PowerHardOff)))

	g.Expect(getHardware(g, c).Labels).To(HaveKey(HardwareOwnerNameLabel), "Hardware released before power off completed")

	tm := &infrastructurev1.TinkerbellMachine{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Name: bmcTestMachineName, Namespace: machineNamespace}, tm)).To(Succeed())
	g.Expect(tm.Finalizers).To(ContainElement(infrastructurev1.MachineFinalizer))

	return job
}

// A TinkerbellMachine recreated with the same name must power the Hardware off again on its
// own delete. Its Hardware lives in another namespace, so no owner reference ties the Job of an
// earlier delete to the earlier TinkerbellMachine, and nothing garbage collects that Job.
func Test_DeleteMachineWithDependencies_powers_off_on_every_delete(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()

	c := newBMCTestClient(g, interceptor.Funcs{}, claimedHardware(bmcTestMachineNamespace), legacyPowerOffJob(bmcTestMachineName, bmcTestMachineNamespace))

	for _, uid := range []types.UID{"uid-1", "uid-2"} {
		jobName := "poweroff-" + string(uid)

		scope := deletingMachineScope(g, c, bmcTestMachineNamespace, uid)
		g.Expect(scope.DeleteMachineWithDependencies()).To(MatchError(ContainSubstring("requeue")),
			"the completed Job of an earlier delete must not stand in for this delete's power off")
		job := expectPowerOffPending(g, c, bmcTestMachineNamespace, jobName)
		g.Expect(job.OwnerReferences).To(BeEmpty(), "owner references can't cross namespaces")

		completeJob(g, c, jobName)
		g.Expect(reloadMachineScope(g, c, bmcTestMachineNamespace).DeleteMachineWithDependencies()).To(Succeed())

		expectJobGone(g, c, jobName)
		expectJobGone(g, c, bmcTestMachineName+"-poweroff")
		expectMachineGone(g, c, bmcTestMachineNamespace)

		hw := getHardware(g, c)
		g.Expect(hw.Labels).NotTo(HaveKey(HardwareOwnerNameLabel))

		// Claim the Hardware again for the next TinkerbellMachine.
		setHardwareOwner(hw, bmcTestMachineName, bmcTestMachineNamespace)
		g.Expect(c.Update(ctx, hw)).To(Succeed())
	}
}

// With the Hardware in the TinkerbellMachine's namespace, the Job has an owner reference, and
// the delete still removes it rather than wait for the garbage collector, which the embedded
// kube-apiserver lacks.
func Test_DeleteMachineWithDependencies_same_namespace(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	c := newBMCTestClient(g, interceptor.Funcs{}, claimedHardware(bmcTestTinkerbellNamespace))

	scope := deletingMachineScope(g, c, bmcTestTinkerbellNamespace, "uid-1")
	g.Expect(scope.DeleteMachineWithDependencies()).To(MatchError(ContainSubstring("requeue")))
	job := expectPowerOffPending(g, c, bmcTestTinkerbellNamespace, "poweroff-uid-1")
	g.Expect(job.OwnerReferences).To(ConsistOf(HaveField("UID", types.UID("uid-1"))))

	completeJob(g, c, "poweroff-uid-1")
	g.Expect(reloadMachineScope(g, c, bmcTestTinkerbellNamespace).DeleteMachineWithDependencies()).To(Succeed())

	expectJobGone(g, c, "poweroff-uid-1")
	expectMachineGone(g, c, bmcTestTinkerbellNamespace)
}

// A delete that fails after releasing the Hardware retries with Hardware another
// TinkerbellMachine may have claimed since. The retry must not power that machine's node off,
// nor take the Hardware from it, and must still remove its own Job.
func Test_DeleteMachineWithDependencies_retry_leaves_reclaimed_hardware_alone(t *testing.T) {
	t.Parallel()

	const (
		failFinalizerPatch = "finalizer patch"
		failJobDelete      = "job delete"
	)

	tests := map[string]struct {
		failingStep               string
		ownerName, ownerNamespace string
	}{
		"finalizer patch fails, another machine in the same namespace claims it": {
			failingStep: failFinalizerPatch, ownerName: "other-machine", ownerNamespace: bmcTestMachineNamespace,
		},
		"finalizer patch fails, a machine of the same name in another namespace claims it": {
			failingStep: failFinalizerPatch, ownerName: bmcTestMachineName, ownerNamespace: "other-ns",
		},
		"job delete fails, another machine claims it": {
			failingStep: failJobDelete, ownerName: "other-machine", ownerNamespace: bmcTestMachineNamespace,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := context.Background()

			// Fail the failing step once, when armed.
			var armed atomic.Bool
			funcs := interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*infrastructurev1.TinkerbellMachine); ok && tc.failingStep == failFinalizerPatch &&
						len(obj.GetFinalizers()) == 0 && armed.CompareAndSwap(true, false) {
						return errors.New("injected failure")
					}

					return c.Patch(ctx, obj, p, opts...)
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*rufiov1.Job); ok && tc.failingStep == failJobDelete && armed.CompareAndSwap(true, false) {
						return errors.New("injected failure")
					}

					return c.Delete(ctx, obj, opts...)
				},
			}
			c := newBMCTestClient(g, funcs, claimedHardware(bmcTestMachineNamespace))

			scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
			g.Expect(scope.DeleteMachineWithDependencies()).To(MatchError(ContainSubstring("requeue")))
			completeJob(g, c, "poweroff-uid-1")

			armed.Store(true)
			g.Expect(reloadMachineScope(g, c, bmcTestMachineNamespace).DeleteMachineWithDependencies()).To(MatchError(ContainSubstring("injected failure")))

			// Another TinkerbellMachine claims the released Hardware before the retry.
			hw := getHardware(g, c)
			g.Expect(hw.Labels).NotTo(HaveKey(HardwareOwnerNameLabel))
			setHardwareOwner(hw, tc.ownerName, tc.ownerNamespace)
			g.Expect(c.Update(ctx, hw)).To(Succeed())

			g.Expect(reloadMachineScope(g, c, bmcTestMachineNamespace).DeleteMachineWithDependencies()).To(Succeed())

			expectJobGone(g, c, "poweroff-uid-1")
			expectMachineGone(g, c, bmcTestMachineNamespace)
			g.Expect(getHardware(g, c).Labels).To(Equal(map[string]string{
				HardwareOwnerNameLabel:      tc.ownerName,
				HardwareOwnerNamespaceLabel: tc.ownerNamespace,
			}))
		})
	}
}

// The delete decides from the API server whether the Hardware is still its own. A cache that
// hasn't seen an earlier attempt release the Hardware, and another machine claim it, would have
// the delete power off that machine's node.
func Test_DeleteMachineWithDependencies_reads_hardware_ownership_without_cache(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cached := newBMCTestClient(g, interceptor.Funcs{}, claimedHardware(bmcTestMachineNamespace))

	current := claimedHardware(bmcTestMachineNamespace)
	setHardwareOwner(current, "other-machine", bmcTestMachineNamespace)
	apiServer := newBMCTestClient(g, interceptor.Funcs{}, current)

	scope := deletingMachineScope(g, cached, bmcTestMachineNamespace, "uid-1")
	scope.tinkerbellReader = apiServer
	g.Expect(scope.DeleteMachineWithDependencies()).To(Succeed())

	expectJobGone(g, cached, "poweroff-uid-1")
	expectMachineGone(g, cached, bmcTestMachineNamespace)
}

// Another TinkerbellMachine can claim the Hardware between the delete reading it and releasing
// it. The release must then fail rather than remove that machine's ownership, and the retry must
// leave the Hardware alone.
func Test_DeleteMachineWithDependencies_release_does_not_undo_a_concurrent_claim(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// When armed, claim the Hardware for another machine just before the release patch lands.
	var armed atomic.Bool
	funcs := interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
			if _, ok := obj.(*tinkv1.Hardware); ok && armed.CompareAndSwap(true, false) {
				current := &tinkv1.Hardware{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
					return fmt.Errorf("getting Hardware: %w", err)
				}
				setHardwareOwner(current, "other-machine", bmcTestMachineNamespace)
				if err := c.Update(ctx, current); err != nil {
					return fmt.Errorf("claiming Hardware: %w", err)
				}
			}

			return c.Patch(ctx, obj, p, opts...)
		},
	}
	c := newBMCTestClient(g, funcs, claimedHardware(bmcTestMachineNamespace))

	scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
	g.Expect(scope.DeleteMachineWithDependencies()).To(MatchError(ContainSubstring("requeue")))
	completeJob(g, c, "poweroff-uid-1")

	armed.Store(true)
	err := reloadMachineScope(g, c, bmcTestMachineNamespace).DeleteMachineWithDependencies()
	g.Expect(apierrors.IsConflict(err)).To(BeTrue(), "expected a Conflict releasing the Hardware, got %v", err)

	g.Expect(reloadMachineScope(g, c, bmcTestMachineNamespace).DeleteMachineWithDependencies()).To(Succeed())

	expectJobGone(g, c, "poweroff-uid-1")
	expectMachineGone(g, c, bmcTestMachineNamespace)
	g.Expect(getHardware(g, c).Labels).To(Equal(map[string]string{
		HardwareOwnerNameLabel:      "other-machine",
		HardwareOwnerNamespaceLabel: bmcTestMachineNamespace,
	}))
}

// The legacy Job name only has the TinkerbellMachine's name in it, so a Job of that name can
// belong to a TinkerbellMachine of the same name in another namespace. Its labels tell.
func Test_removePowerOffJobs_keeps_legacy_job_of_another_machine(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		labelName, labelNamespace string
	}{
		"labeled for another namespace": {labelName: bmcTestMachineName, labelNamespace: "other-mgmt-ns"},
		"labeled for another machine":   {labelName: "other-machine", labelNamespace: bmcTestMachineNamespace},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			c := newBMCTestClient(g, interceptor.Funcs{}, claimedHardware(bmcTestMachineNamespace), legacyPowerOffJob(tc.labelName, tc.labelNamespace))

			scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
			g.Expect(scope.removePowerOffJobs()).To(Succeed())

			_, err := getJob(c, bmcTestMachineName+"-poweroff")
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

// A TinkerbellMachine deleted before Hardware selection completed has no Hardware name. The
// uncached reader, like the API server, rejects a read with an empty name rather than report the
// Hardware not found, and the delete must still complete.
func Test_DeleteMachineWithDependencies_without_hardware_name(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	c := newBMCTestClient(g, interceptor.Funcs{})
	apiServer := newBMCTestClient(g, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Name == "" {
				return errors.New("resource name may not be empty")
			}

			return c.Get(ctx, key, obj, opts...)
		},
	})

	scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
	scope.tinkerbellMachine.Spec.HardwareName = ""
	scope.tinkerbellReader = apiServer
	g.Expect(scope.DeleteMachineWithDependencies()).To(Succeed())

	expectMachineGone(g, c, bmcTestMachineNamespace)
}

// The delete removes its power off Job on every path, here when the Hardware lost its BMC
// reference after the Job was created, to unblock a delete whose Job failed.
func Test_DeleteMachineWithDependencies_without_bmc_ref_removes_job(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()

	c := newBMCTestClient(g, interceptor.Funcs{}, claimedHardware(bmcTestMachineNamespace))

	scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
	g.Expect(scope.DeleteMachineWithDependencies()).To(MatchError(ContainSubstring("requeue")))

	hw := getHardware(g, c)
	hw.Spec.BMCRef = nil
	g.Expect(c.Update(ctx, hw)).To(Succeed())

	g.Expect(reloadMachineScope(g, c, bmcTestMachineNamespace).DeleteMachineWithDependencies()).To(Succeed())

	expectJobGone(g, c, "poweroff-uid-1")
	expectMachineGone(g, c, bmcTestMachineNamespace)
	g.Expect(getHardware(g, c).Labels).NotTo(HaveKey(HardwareOwnerNameLabel))
}

// The Hardware can be deleted after the delete created its power off Job. The delete then
// removes the Job, and a legacy Job left for this TinkerbellMachine, without the Hardware.
func Test_DeleteMachineWithDependencies_without_hardware_removes_jobs(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	job := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "poweroff-uid-1", Namespace: bmcTestTinkerbellNamespace},
	}
	c := newBMCTestClient(g, interceptor.Funcs{}, job, legacyPowerOffJob(bmcTestMachineName, bmcTestMachineNamespace))

	scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
	g.Expect(scope.DeleteMachineWithDependencies()).To(Succeed())

	expectJobGone(g, c, "poweroff-uid-1")
	expectJobGone(g, c, bmcTestMachineName+"-poweroff")
	expectMachineGone(g, c, bmcTestMachineNamespace)
}

// A delete that finds no power off Job to remove must not need to delete Jobs: Roles written
// before CAPT deleted Jobs don't grant it.
func Test_DeleteMachineWithDependencies_without_job_needs_no_job_delete(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()

	forbidJobDelete := interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*rufiov1.Job); ok {
				return apierrors.NewForbidden(rufiov1.GroupVersion.WithResource("jobs").GroupResource(), obj.GetName(), errors.New("delete not granted"))
			}

			return c.Delete(ctx, obj, opts...)
		},
	}
	hw := claimedHardware(bmcTestMachineNamespace)
	hw.Spec.BMCRef = nil
	c := newBMCTestClient(g, forbidJobDelete, hw)

	scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
	g.Expect(scope.DeleteMachineWithDependencies()).To(Succeed())

	expectMachineGone(g, c, bmcTestMachineNamespace)
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(hw), hw)).To(Succeed())
	g.Expect(hw.Labels).NotTo(HaveKey(HardwareOwnerNameLabel))
}

// The power off Job is read without the cache, which may not show a Job created a reconcile
// earlier: the delete would then leave it behind.
func Test_removePowerOffJobs_reads_job_without_cache(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	var deleted []string
	recordJobDelete := interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*rufiov1.Job); ok {
				deleted = append(deleted, obj.GetName())
			}

			return c.Delete(ctx, obj, opts...)
		},
	}
	cached := newBMCTestClient(g, recordJobDelete)

	job := &rufiov1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "poweroff-uid-1", Namespace: bmcTestTinkerbellNamespace},
	}
	apiServer := newBMCTestClient(g, interceptor.Funcs{}, job)

	scope := deletingMachineScope(g, cached, bmcTestMachineNamespace, "uid-1")
	scope.tinkerbellReader = apiServer
	g.Expect(scope.removePowerOffJobs()).To(Succeed())
	g.Expect(deleted).To(ConsistOf("poweroff-uid-1"))
}

// An error reading the power off Job, other than not found, fails the delete instead of
// leaving the Job behind.
func Test_removePowerOffJobs_fails_when_job_read_fails(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	c := newBMCTestClient(g, interceptor.Funcs{})
	unavailable := newBMCTestClient(g, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*rufiov1.Job); ok {
				return apierrors.NewServiceUnavailable("unavailable")
			}

			return c.Get(ctx, key, obj, opts...)
		},
	})

	scope := deletingMachineScope(g, c, bmcTestMachineNamespace, "uid-1")
	scope.tinkerbellReader = unavailable
	g.Expect(scope.removePowerOffJobs()).To(MatchError(ContainSubstring("getting BMCJob")))
}

// Rufio copies the Job name into a label value on the Job's Tasks, and names the Tasks after the
// Job, so the name must be valid in both places whatever the TinkerbellMachine is called.
func Test_powerOffJobName_is_valid_for_rufio(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	scope := &machineReconcileScope{
		tinkerbellMachine: &infrastructurev1.TinkerbellMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("m", validation.DNS1123SubdomainMaxLength),
				UID:  "3f8a1c52-9d6e-4b7a-8e21-5c0d9f4a7b63",
			},
		},
	}

	name := scope.powerOffJobName()
	g.Expect(validation.IsValidLabelValue(name)).To(BeEmpty())
	g.Expect(validation.IsDNS1123Subdomain(name + "-task-0")).To(BeEmpty())
}
