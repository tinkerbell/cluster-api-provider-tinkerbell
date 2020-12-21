/*
Copyright 2020 The Kubernetes Authors.

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
	"io"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/controllers"
	"github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/client"
	tinkhardware "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/hardware"
	tinktemplate "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/template"
	tinkworkflow "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/controllers/workflow"
	tinkclient "github.com/tinkerbell/tink/client"
	tinkinformers "github.com/tinkerbell/tink/client/informers"
	tinkevents "github.com/tinkerbell/tink/protos/events"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	infrastructurev1alpha3 "github.com/tinkerbell/cluster-api-provider-tinkerbell/api/v1alpha3"
	tinkv1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	// +kubebuilder:scaffold:imports
)

//nolint:gochecknoglobals
var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

//nolint:wsl,gochecknoinits
func init() {
	klog.InitFlags(nil)

	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrastructurev1alpha3.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = tinkv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

//nolint:funlen,gomnd
func main() {
	var (
		enableLeaderElection    bool
		leaderElectionNamespace string
		healthAddr              string
		metricsAddr             string
		webhookPort             int
		syncPeriod              time.Duration
		watchNamespace          string
	)

	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(
		&leaderElectionNamespace,
		"leader-election-namespace",
		"",
		"Namespace that the controller performs leader election in. "+
			"If unspecified, the controller will discover which namespace it is running in.",
	)

	flag.StringVar(&healthAddr,
		"health-addr",
		":9440",
		"The address the health endpoint binds to.",
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")

	flag.DurationVar(&syncPeriod,
		"sync-period",
		10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)",
	)

	flag.StringVar(
		&watchNamespace,
		"namespace",
		"",
		"Namespace that the controller watches to reconcile cluster-api objects. "+
			"If unspecified, the controller watches for cluster-api objects across all namespaces.",
	)

	flag.IntVar(&webhookPort,
		"webhook-port",
		0,
		"Webhook Server port, disabled by default. When enabled, the manager will only "+
			"work as webhook server, no reconcilers are installed.",
	)

	flag.Parse()

	ctrl.SetLogger(klogr.New())

	if watchNamespace != "" {
		setupLog.Info("Watching cluster-api objects only in namespace for reconciliation", "namespace", watchNamespace)
	}

	// Machine and cluster operations can create enough events to trigger the event recorder spam filter
	// Setting the burst size higher ensures all events will be recorded and submitted to the API
	broadcaster := record.NewBroadcasterWithCorrelatorOptions(record.CorrelatorOptions{
		BurstSize: 100,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		Port:                    webhookPort,
		EventBroadcaster:        broadcaster,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "controller-leader-election-capt",
		LeaderElectionNamespace: leaderElectionNamespace,
		Namespace:               watchNamespace,
		SyncPeriod:              &syncPeriod,
		HealthProbeBindAddress:  healthAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := tinkclient.Setup(); err != nil {
		setupLog.Error(err, "unable to create tinkerbell client")
		os.Exit(1)
	}

	hwClient := client.NewHardwareClient(tinkclient.HardwareClient)
	templateClient := client.NewTemplateClient(tinkclient.TemplateClient)
	workflowClient := client.NewWorkflowClient(tinkclient.WorkflowClient, hwClient)

	stopCh := ctrl.SetupSignalHandler()

	if webhookPort == 0 {
		hwChan := make(chan event.GenericEvent)
		templateChan := make(chan event.GenericEvent)
		workflowChan := make(chan event.GenericEvent)

		if err := mgr.Add(&tinkEventWatcher{
			k8sClient:    mgr.GetClient(),
			eventCh:      hwChan,
			logger:       ctrl.Log.WithName("tinkwatcher").WithName("Hardware"),
			resourceType: tinkevents.ResourceType_RESOURCE_TYPE_HARDWARE,
		}); err != nil {
			setupLog.Error(err, "unable to create tink watcher", "tinkwatcher", "Hardware")
			os.Exit(1)
		}

		if err := mgr.Add(&tinkEventWatcher{
			k8sClient:    mgr.GetClient(),
			eventCh:      templateChan,
			logger:       ctrl.Log.WithName("tinkwatcher").WithName("Template"),
			resourceType: tinkevents.ResourceType_RESOURCE_TYPE_TEMPLATE,
		}); err != nil {
			setupLog.Error(err, "unable to create tink watcher", "tinkwatcher", "Template")
			os.Exit(1)
		}

		if err := mgr.Add(&tinkEventWatcher{
			k8sClient:    mgr.GetClient(),
			eventCh:      workflowChan,
			logger:       ctrl.Log.WithName("tinkwatcher").WithName("Workflow"),
			resourceType: tinkevents.ResourceType_RESOURCE_TYPE_WORKFLOW,
		}); err != nil {
			setupLog.Error(err, "unable to create tink watcher", "tinkwatcher", "Workflow")
			os.Exit(1)
		}

		if err = (&tinkhardware.Reconciler{
			Client:         mgr.GetClient(),
			HardwareClient: hwClient,
			Log:            ctrl.Log.WithName("controllers").WithName("Hardware"),
			Scheme:         mgr.GetScheme(),
		}).SetupWithManager(mgr, hwChan); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Hardware")
			os.Exit(1)
		}

		if err = (&tinktemplate.Reconciler{
			Client:         mgr.GetClient(),
			TemplateClient: templateClient,
			Log:            ctrl.Log.WithName("controllers").WithName("Template"),
			Scheme:         mgr.GetScheme(),
		}).SetupWithManager(mgr, templateChan); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Template")
			os.Exit(1)
		}

		if err = (&tinkworkflow.Reconciler{
			Client:         mgr.GetClient(),
			WorkflowClient: workflowClient,
			Log:            ctrl.Log.WithName("controllers").WithName("Workflow"),
			Scheme:         mgr.GetScheme(),
		}).SetupWithManager(mgr, workflowChan); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Workflow")
			os.Exit(1)
		}

		if err = (&controllers.TinkerbellClusterReconciler{
			Client:   mgr.GetClient(),
			Log:      ctrl.Log.WithName("controllers").WithName("TinkerbellCluster"),
			Recorder: mgr.GetEventRecorderFor("tinkerbellcluster-controller"),
			Scheme:   mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "TinkerbellCluster")
			os.Exit(1)
		}

		if err = (&controllers.TinkerbellMachineReconciler{
			Client:   mgr.GetClient(),
			Log:      ctrl.Log.WithName("controllers").WithName("TinkerbellMachine"),
			Scheme:   mgr.GetScheme(),
			Recorder: mgr.GetEventRecorderFor("tinkerbellmachine-controller"),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "TinkerbellMachine")
			os.Exit(1)
		}
	} else {
		// TODO: add the webhook configuration
		setupLog.Error(errors.New("webhook not implemented"), "webhook", "not available")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	if err := mgr.Start(stopCh); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

type tinkEventWatcher struct {
	k8sClient    ctrlclient.Client
	eventCh      chan<- event.GenericEvent
	logger       logr.Logger
	resourceType tinkevents.ResourceType
}

func (w *tinkEventWatcher) getHardwareForID(ctx context.Context, id string) (*tinkv1.Hardware, error) {
	hwList := &tinkv1.HardwareList{}
	if err := w.k8sClient.List(ctx, hwList); err != nil {
		return nil, err
	}

	for i, h := range hwList.Items {
		if h.TinkID() == id {
			return &hwList.Items[i], nil
		}
	}

	return nil, nil
}

func (w *tinkEventWatcher) getTemplateForID(ctx context.Context, id string) (*tinkv1.Template, error) {
	templateList := &tinkv1.TemplateList{}
	if err := w.k8sClient.List(ctx, templateList); err != nil {
		return nil, err
	}

	for i, t := range templateList.Items {
		if t.TinkID() == id {
			return &templateList.Items[i], nil
		}
	}

	return nil, nil
}

func (w *tinkEventWatcher) getWorkflowForID(ctx context.Context, id string) (*tinkv1.Workflow, error) {
	workflowList := &tinkv1.WorkflowList{}
	if err := w.k8sClient.List(ctx, workflowList); err != nil {
		return nil, err
	}

	for i, w := range workflowList.Items {
		if w.TinkID() == id {
			return &workflowList.Items[i], nil
		}
	}

	return nil, nil
}

func (w *tinkEventWatcher) generateEventForTinkID(ctx context.Context, id string) error {
	switch w.resourceType {
	case tinkevents.ResourceType_RESOURCE_TYPE_HARDWARE:
		hw, _ := w.getHardwareForID(ctx, id)
		w.eventCh <- event.GenericEvent{
			Meta:   hw,
			Object: hw,
		}
	case tinkevents.ResourceType_RESOURCE_TYPE_TEMPLATE:
		template, _ := w.getTemplateForID(ctx, id)
		w.eventCh <- event.GenericEvent{
			Meta:   template,
			Object: template,
		}
	case tinkevents.ResourceType_RESOURCE_TYPE_WORKFLOW:
		workflow, _ := w.getWorkflowForID(ctx, id)
		w.eventCh <- event.GenericEvent{
			Meta:   workflow,
			Object: workflow,
		}
	default:
		return fmt.Errorf("unknown resource type: %s", w.resourceType.String())
	}

	return nil
}

func (w *tinkEventWatcher) Start(stopCh <-chan struct{}) error {
	now := time.Now()

	req := &tinkevents.WatchRequest{
		EventTypes: []tinkevents.EventType{
			tinkevents.EventType_EVENT_TYPE_CREATED,
			tinkevents.EventType_EVENT_TYPE_UPDATED,
			tinkevents.EventType_EVENT_TYPE_DELETED,
		},
		ResourceTypes:   []tinkevents.ResourceType{w.resourceType},
		WatchEventsFrom: timestamppb.New(now),
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-stopCh
		cancel()
	}()

	tinkInformer := tinkinformers.New()

	w.logger.Info("Starting Tinkerbell Informer", "resourceType", w.resourceType.String())

	err := tinkInformer.Start(ctx, req, func(e *tinkevents.Event) error {
		if err := w.generateEventForTinkID(ctx, e.GetResourceId()); err != nil {
			return err
		}

		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}
