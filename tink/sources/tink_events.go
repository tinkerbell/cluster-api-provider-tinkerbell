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

// Package sources contains custom controller-runtime sources for Tinkerbell.
package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-logr/logr"
	tinkinformers "github.com/tinkerbell/tink/client/informers"
	tinkevents "github.com/tinkerbell/tink/protos/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	tinkv1 "github.com/tinkerbell/cluster-api-provider-tinkerbell/tink/api/v1alpha1"
)

// TinkEventWatcher is a source that watches for Tinkerbell events and generates a GenericEvent
// for the associated CRD resource.
type TinkEventWatcher struct {
	client       client.Client
	Logger       logr.Logger
	EventCh      chan<- event.GenericEvent
	ResourceType tinkevents.ResourceType
}

func (w *TinkEventWatcher) getHardwareForID(ctx context.Context, id string) (*tinkv1.Hardware, error) {
	hwList := &tinkv1.HardwareList{}
	if err := w.client.List(ctx, hwList); err != nil {
		return nil, fmt.Errorf("failed to list hardware: %w", err)
	}

	for i, h := range hwList.Items {
		if h.TinkID() == id {
			w.Logger.Info("generating GenericEvent", "hardware", h.GetName())

			return &hwList.Items[i], nil
		}
	}

	return nil, nil
}

func (w *TinkEventWatcher) getTemplateForID(ctx context.Context, id string) (*tinkv1.Template, error) {
	templateList := &tinkv1.TemplateList{}
	if err := w.client.List(ctx, templateList); err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}

	for i, t := range templateList.Items {
		if t.TinkID() == id {
			w.Logger.Info("generating GenericEvent", "template", t.GetName())

			return &templateList.Items[i], nil
		}
	}

	return nil, nil
}

func (w *TinkEventWatcher) getWorkflowForID(ctx context.Context, id string) (*tinkv1.Workflow, error) {
	workflowList := &tinkv1.WorkflowList{}
	if err := w.client.List(ctx, workflowList); err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	for i, wf := range workflowList.Items {
		if wf.TinkID() == id {
			w.Logger.Info("generating GenericEvent", "workflow", wf.GetName())

			return &workflowList.Items[i], nil
		}
	}

	return nil, nil
}

func (w *TinkEventWatcher) generateEventForTinkID(ctx context.Context, id string) error {
	switch w.ResourceType {
	case tinkevents.ResourceType_RESOURCE_TYPE_HARDWARE:
		hw, err := w.getHardwareForID(ctx, id)
		if err != nil {
			return err
		}

		if hw != nil {
			w.EventCh <- event.GenericEvent{
				Meta:   hw,
				Object: hw,
			}
		}
	case tinkevents.ResourceType_RESOURCE_TYPE_TEMPLATE:
		template, err := w.getTemplateForID(ctx, id)
		if err != nil {
			return err
		}

		if template != nil {
			w.EventCh <- event.GenericEvent{
				Meta:   template,
				Object: template,
			}
		}
	case tinkevents.ResourceType_RESOURCE_TYPE_WORKFLOW:
		workflow, err := w.getWorkflowForID(ctx, id)
		if err != nil {
			return err
		}

		if workflow != nil {
			w.EventCh <- event.GenericEvent{
				Meta:   workflow,
				Object: workflow,
			}
		}
	default:
		return fmt.Errorf("unknown resource type: %s", w.ResourceType.String())
	}

	return nil
}

// NeedLeaderElection satisfies the controller-runtime LeaderElectionRunnable interface.
func (w *TinkEventWatcher) NeedLeaderElection() bool {
	return true
}

// InjectClient satisfies the controller-runtime Client injection interface.
func (w *TinkEventWatcher) InjectClient(c client.Client) error {
	w.client = c

	return nil
}

// Start starts the TinkEventWatcher.
func (w *TinkEventWatcher) Start(stopCh <-chan struct{}) error {
	// TODO: currently this only triggers events for
	// changes to workflows themselves, but not for updates
	// to workflow_events, workflow_state, or workflow_data for
	// a given workflow, need to figure out a way to trigger
	// events for those.
	now := time.Now()

	req := &tinkevents.WatchRequest{
		EventTypes: []tinkevents.EventType{
			tinkevents.EventType_EVENT_TYPE_CREATED,
			tinkevents.EventType_EVENT_TYPE_UPDATED,
			tinkevents.EventType_EVENT_TYPE_DELETED,
		},
		ResourceTypes:   []tinkevents.ResourceType{w.ResourceType},
		WatchEventsFrom: timestamppb.New(now),
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-stopCh
		cancel()
	}()

	tinkInformer := tinkinformers.New()

	w.Logger.Info("Starting Tinkerbell Informer", "resourceType", w.ResourceType.String())

	err := tinkInformer.Start(ctx, req, func(e *tinkevents.Event) error {
		if err := w.generateEventForTinkID(ctx, e.GetResourceId()); err != nil {
			return err
		}

		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) && status.Code(err) != codes.Canceled {
		return fmt.Errorf("unexpected error from Tinkerbell informer: %w", err)
	}

	return nil
}
