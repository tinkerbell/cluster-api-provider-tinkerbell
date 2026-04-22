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
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zerologr"
	"github.com/peterbourgon/ff/v3"
	"github.com/rs/zerolog"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	cgrecord "k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	infrastructurev1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1beta1"
	captctrl "github.com/tinkerbell/cluster-api-provider-tinkerbell/controller"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controller/cluster"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controller/machine"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/pkg/build"
	tinkcluster "github.com/tinkerbell/cluster-api-provider-tinkerbell/pkg/cluster"
)

type config struct {
	EnableLeaderElection          bool
	MetricsBindAddress            string
	LeaderElectionNamespace       string
	WatchNamespace                string
	HealthAddr                    string
	WatchFilterValue              string
	WebhookCertDir                string
	TinkerbellClusterConcurrency  int
	TinkerbellMachineConcurrency  int
	TinkerbellHardwareConcurrency int
	TinkerbellTemplateConcurrency int
	TinkerbellWorkflowConcurrency int
	WebhookPort                   int
	SyncPeriod                    time.Duration
	LeaderElectionLeaseDuration   time.Duration
	LeaderElectionRenewDeadline   time.Duration
	LeaderElectionRetryPeriod     time.Duration
	ExternalKubeconfig            string
}

type tinkerbellClientResult struct {
	Client       client.Client
	External     bool
	WatchManager *tinkcluster.NamespaceWatchManager
}

func main() {
	// Set up logging before any log calls so all startup messages are visible.
	zl := zerolog.New(os.Stdout).Level(zerolog.InfoLevel).With().Caller().Timestamp().Logger()
	ctrl.SetLogger(zerologr.New(&zl))
	klog.SetLogger(ctrl.Log)

	log := ctrl.Log.WithName("setup")

	fs := flag.NewFlagSet("capt", flag.ExitOnError)
	klog.InitFlags(fs)
	// Opt into the new klog behavior so that -stderrthreshold is honored even
	// when -logtostderr=true (the default).
	// Ref: kubernetes/klog#212, kubernetes/klog#432
	_ = fs.Set("legacy_stderr_threshold_behavior", "false")
	_ = fs.Set("stderrthreshold", "INFO")

	cfg := &config{}
	cfg.initFlags(fs)

	if err := ff.Parse(fs, os.Args[1:], ff.WithEnvVarNoPrefix(), ff.WithConfigFileParser(ff.PlainParser)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if cfg.WatchNamespace != "" {
		log.Info("Watching cluster-api objects only in namespace for reconciliation", "namespace", cfg.WatchNamespace)
	}

	rs, err := newScheme()
	if err != nil {
		log.Error(err, "failed to setup runtime scheme")
		os.Exit(1)
	}

	// Machine and cluster operations can create enough events to trigger the event recorder spam filter
	// Setting the burst size higher ensures all events will be recorded and submitted to the API
	broadcaster := cgrecord.NewBroadcasterWithCorrelatorOptions(cgrecord.CorrelatorOptions{
		BurstSize: 100,
	})
	opts := ctrl.Options{
		Scheme: rs,
		Metrics: metricsserver.Options{
			BindAddress: cfg.MetricsBindAddress,
		},
		LeaderElection:          cfg.EnableLeaderElection,
		LeaderElectionID:        "controller-leader-election-capt",
		LeaderElectionNamespace: cfg.LeaderElectionNamespace,
		LeaseDuration:           &cfg.LeaderElectionLeaseDuration,
		RenewDeadline:           &cfg.LeaderElectionRenewDeadline,
		RetryPeriod:             &cfg.LeaderElectionRetryPeriod,
		HealthProbeBindAddress:  cfg.HealthAddr,
		EventBroadcaster:        broadcaster,
	}

	if cfg.WatchNamespace != "" {
		opts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{cfg.WatchNamespace: {}},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		log.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize event recorder.
	// GetEventRecorderFor is deprecated in favor of GetEventRecorder, but
	// record.InitFromRecorder requires the old record.EventRecorder type.
	//nolint:staticcheck // SA1019
	record.InitFromRecorder(mgr.GetEventRecorderFor("tinkerbell-controller"))

	// Setup the context that's going to be used in controllers and for the manager.
	ctx := ctrl.SetupSignalHandler()

	if err := cfg.setupReconcilers(ctx, log, rs, mgr); err != nil {
		log.Error(err, "failed to add Tinkerbell Reconcilers")
		os.Exit(1)
	}

	if err := setupWebhooks(mgr); err != nil {
		log.Error(err, "failed to add Tinkerbell Webhooks")
		os.Exit(1)
	}

	if err := addHealthChecks(mgr); err != nil {
		log.Error(err, "failed to add health checks")
		os.Exit(1)
	}

	log.Info("starting manager", "version", build.GitRevision())

	if err := mgr.Start(ctx); err != nil {
		log.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func (c *config) setupReconcilers(ctx context.Context, log logr.Logger, rs *runtime.Scheme, mgr ctrl.Manager) error {
	log.Info("Setting up kubernetes clients for controllers")

	result, err := c.buildTinkerbellClient(ctx, log, rs, mgr)
	if err != nil {
		return err
	}

	if err := (&cluster.TinkerbellClusterReconciler{
		Client:           mgr.GetClient(),
		WatchFilterValue: c.WatchFilterValue,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: c.TinkerbellClusterConcurrency}, rs); err != nil {
		return fmt.Errorf("unable to setup TinkerbellCluster controller:%w", err)
	}

	if err := (&machine.TinkerbellMachineReconciler{
		Client:             mgr.GetClient(),
		TinkerbellClient:   result.Client,
		ExternalTinkerbell: result.External,
		WatchManager:       result.WatchManager,
		Scheme:             rs,
		WatchFilterValue:   c.WatchFilterValue,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: c.TinkerbellMachineConcurrency}, rs); err != nil {
		return fmt.Errorf("unable to setup TinkerbellMachine controller:%w", err)
	}

	return nil
}

func (c *config) buildTinkerbellClient(ctx context.Context, setupLog logr.Logger, rs *runtime.Scheme, mgr ctrl.Manager) (tinkerbellClientResult, error) {
	tinkClient := mgr.GetClient()

	restConfig, restErr := tinkcluster.RestConfig(c.ExternalKubeconfig)
	if restErr != nil && !errors.Is(restErr, tinkcluster.NoConfigError{}) {
		return tinkerbellClientResult{}, fmt.Errorf("failed to build external Tinkerbell client: %w", restErr)
	}

	if errors.Is(restErr, tinkcluster.NoConfigError{}) {
		setupLog.Info("using local Tinkerbell for CRD operations", "tinkerbellClientMode", "local", "reason", restErr.Error())
		return tinkerbellClientResult{Client: tinkClient}, nil
	}

	setupLog.Info("using external Tinkerbell with JIT per-namespace watches",
		"tinkerbellClientMode", "external",
	)
	directClient, dErr := tinkcluster.NewDirectClient(restConfig, rs)
	if dErr != nil {
		return tinkerbellClientResult{}, fmt.Errorf("failed to create external Tinkerbell client: %w", dErr)
	}
	wm := tinkcluster.NewNamespaceWatchManager(
		restConfig, rs,
		machine.LabelMachineName, machine.LabelMachineNamespace,
	)
	wm.SetContext(ctx)
	return tinkerbellClientResult{Client: directClient, External: true, WatchManager: wm}, nil
}

func setupWebhooks(mgr ctrl.Manager) error {
	if err := (&infrastructurev1.TinkerbellCluster{}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to setup TinkerbellCluster webhook:%w", err)
	}

	if err := (&infrastructurev1.TinkerbellMachine{}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to setup TinkerbellMachine webhook:%w", err)
	}

	if err := (&infrastructurev1.TinkerbellMachineTemplate{}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to setup TinkerbellMachineTemplate webhook:%w", err)
	}

	return nil
}

func addHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		return fmt.Errorf("unable to create ready check: %w", err)
	}

	if err := mgr.AddHealthzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		return fmt.Errorf("unable to create healthz check: %w", err)
	}

	return nil
}

func (c *config) initFlags(fs *flag.FlagSet) { //nolint:funlen
	fs.StringVar(
		&c.MetricsBindAddress,
		"metrics-bind-addr",
		"localhost:8080",
		"The address the metric endpoint binds to.",
	)

	fs.BoolVar(
		&c.EnableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.",
	)

	fs.DurationVar(
		&c.LeaderElectionLeaseDuration,
		"leader-elect-lease-duration",
		15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)",
	)

	fs.DurationVar(
		&c.LeaderElectionRenewDeadline,
		"leader-elect-renew-deadline",
		10*time.Second,
		"Duration that the leading controller manager will retry refreshing leadership before giving up (duration string)",
	)

	fs.DurationVar(
		&c.LeaderElectionRetryPeriod,
		"leader-elect-retry-period",
		2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)",
	)

	fs.StringVar(
		&c.WatchNamespace,
		"namespace",
		"",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.",
	)

	fs.StringVar(
		&c.LeaderElectionNamespace,
		"leader-election-namespace",
		"",
		"Namespace that the controller performs leader election in. If unspecified, the controller will discover which namespace it is running in.",
	)

	fs.StringVar(
		&c.WatchFilterValue,
		"watch-filter",
		"",
		fmt.Sprintf("Label value that the controller watches to reconcile cluster-api objects. Label key is always %s. If unspecified, the controller watches for all cluster-api objects.", clusterv1.WatchLabel), //nolint:lll
	)

	fs.IntVar(&c.TinkerbellClusterConcurrency,
		"tinkerbellcluster-concurrency",
		10,
		"Number of TinkerbellClusters to process simultaneously",
	)

	fs.IntVar(&c.TinkerbellMachineConcurrency,
		"tinkerbellmachine-concurrency",
		10,
		"Number of TinkerbellMachines to process simultaneously",
	)

	fs.IntVar(&c.TinkerbellHardwareConcurrency,
		"tinkerbell-hardware-concurrency",
		10,
		"Number of Tinkerbell Hardware resources to process simultaneously",
	)

	fs.IntVar(&c.TinkerbellTemplateConcurrency,
		"tinkerbell-template-concurrency",
		10,
		"Number of Tinkerbell Template resources to process simultaneously",
	)

	fs.IntVar(&c.TinkerbellWorkflowConcurrency,
		"tinkerbell-workflow-concurrency",
		10,
		"Number of Tinkerbell Workflow resources to process simultaneously",
	)

	fs.DurationVar(&c.SyncPeriod,
		"sync-period",
		10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)",
	)

	fs.IntVar(&c.WebhookPort,
		"webhook-port",
		9443,
		"Webhook Server port",
	)

	fs.StringVar(&c.WebhookCertDir,
		"webhook-cert-dir",
		"/tmp/k8s-webhook-server/serving-certs",
		"Webhook Server Certificate Directory, is the directory that contains the server key and certificate",
	)

	fs.StringVar(&c.HealthAddr,
		"health-addr",
		":9440",
		"The address the health endpoint binds to.",
	)

	fs.StringVar(&c.ExternalKubeconfig,
		"external-kubeconfig",
		"/var/run/secrets/external-tinkerbell/kubeconfig",
		"Path to a kubeconfig file for an external Tinkerbell cluster.",
	)
}

// newScheme returns a runtime.Scheme with all CAPT needed schemes added.
func newScheme() (*runtime.Scheme, error) {
	rs := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(rs); err != nil {
		return nil, fmt.Errorf("failed to add client-go scheme: err: %w", err)
	}
	if err := infrastructurev1.AddToScheme(rs); err != nil {
		return nil, fmt.Errorf("failed to add infrastructurev1 scheme: err: %w", err)
	}
	if err := clusterv1.AddToScheme(rs); err != nil {
		return nil, fmt.Errorf("failed to add clusterv1 scheme: err: %w", err)
	}
	if err := captctrl.AddToSchemeTinkerbell(rs); err != nil {
		return nil, fmt.Errorf("failed to add Tinkerbell scheme: err: %w", err)
	}
	if err := captctrl.AddToSchemeBMC(rs); err != nil {
		return nil, fmt.Errorf("failed to add BMC scheme: err: %w", err)
	}
	return rs, nil
}
