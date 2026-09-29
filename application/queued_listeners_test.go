package application_test

import (
	"context"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
)

type signupSubscriber struct {
	topic  events.Topic[assemblyPayload]
	record func(string)
}

func (s signupSubscriber) Declarations() ([]events.Declaration, error) {
	declaration, err := s.topic.Declare(events.Listen("subscriber.audit", func(_ context.Context, p assemblyPayload) error { s.record("audit:" + p.Value); return nil }))
	return []events.Declaration{declaration}, err
}

func TestConfiguredQueuedListenersAndSubscribersRunThroughJobs(t *testing.T) {
	s := settings()
	s.HTTP.Enabled = false
	s.Features.Events.Enabled = true
	s.Features.Events.QueuedListeners = true
	c := infrastructure.DefaultJobConnectionSettings()
	c.Driver = infrastructure.SyncJobs
	s.Services.Jobs.Connections = infrastructure.JobConnections{"default": c}
	topic := events.Define[assemblyPayload]("signed.up", 1)
	var mu sync.Mutex
	var order []string
	record := func(entry string) { mu.Lock(); defer mu.Unlock(); order = append(order, entry) }
	app, err := application.New(s, quiet()).Events(
		application.Subscribe("signup", func(application.Services) (events.Subscriber, error) {
			return signupSubscriber{topic: topic, record: record}, nil
		}),
		application.ListenQueued(topic, "welcome", func(application.Services) (events.Handler[assemblyPayload], error) {
			return func(_ context.Context, p assemblyPayload) error { record("welcome:" + p.Value); return nil }, nil
		}),
	).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(t, app) })
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	bus, err := app.Resources().Events()
	if err != nil {
		t.Fatal(err)
	}
	if err := topic.Dispatch(t.Context(), bus, assemblyPayload{"ada"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 {
		t.Fatal("subscriber or queued listener did not run", order)
	}
	seen := map[string]bool{order[0]: true, order[1]: true}
	if !seen["audit:ada"] || !seen["welcome:ada"] {
		t.Fatal("listener deliveries", order)
	}
}
