package jobs_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/jobs/memory"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"testing"
)

func TestBoundConnectionQueueIdentityIsolationAndSchedule(t *testing.T) {
	definition := jobs.Define[payload]("bound", 1, jobs.DefaultPolicy("declared"))
	makeConnection := func(name string) *jobs.Connection {
		backend, err := memory.New(memory.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		d, err := definition.Declare(nil)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := jobs.NewRegistry(d)
		if err != nil {
			t.Fatal(err)
		}
		dispatcher, err := jobs.NewDispatcher(backend, registry, jobs.DefaultDispatchConfig(keyspace.Namespace{Application: name, Environment: "test"}))
		if err != nil {
			t.Fatal(err)
		}
		c, err := jobs.NewConnection(dispatcher, "configured")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second := makeConnection("first"), makeConnection("second")
	bound, err := definition.On(first)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := bound.Capture(t.Context(), payload{}, jobs.Options[payload]{})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Envelope().Queue() != "configured" {
		t.Fatal("bound capture used declaration queue")
	}
	receipt, err := bound.Dispatch(t.Context(), payload{}, jobs.Options[payload]{ID: pending.ID()})
	if err != nil || receipt.ID != pending.ID() {
		t.Fatal("stable ID lost", err)
	}
	duplicate, err := bound.Dispatch(t.Context(), payload{}, jobs.Options[payload]{ID: pending.ID()})
	if err != nil || duplicate.Inserted {
		t.Fatal("duplicate dispatch changed identity", err)
	}
	other, _ := definition.On(second)
	record, err := other.Inspect(t.Context(), receipt.ID, "")
	if err != nil || record.IsSet() {
		t.Fatal("connection routing leaked", err)
	}
	explicit, err := bound.Dispatch(t.Context(), payload{}, jobs.Options[payload]{Queue: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	record, err = bound.Inspect(t.Context(), explicit.ID, "explicit")
	if err != nil || !record.IsSet() {
		t.Fatal("explicit queue ignored", err)
	}
	foreign, err := second.Outbox("foreign")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Enqueue(t.Context(), nil, foreign, payload{}, jobs.Options[payload]{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("foreign outbox accepted", err)
	}
	if _, err := foreign.PublicationRoute(); !errors.Is(err, fault.Invalid) {
		t.Fatal("memory promised durable acceptance", err)
	}
	handler, err := schedule.ConnectionJobTarget(first, definition, "", func(context.Context, schedule.Invocation) (payload, error) { return payload{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err := model.NewID[schedule.Occurrence]()
	if err != nil {
		t.Fatal(err)
	}
	if err := handler(t.Context(), schedule.Invocation{Occurrence: occurrence}); err != nil {
		t.Fatal(err)
	}
	if err := handler(t.Context(), schedule.Invocation{Occurrence: occurrence}); err != nil {
		t.Fatal(err)
	}
	record, err = bound.Inspect(t.Context(), model.IDFromBytes[jobs.ExecutionOf[payload]](occurrence.Bytes()), "")
	if err != nil || !record.IsSet() {
		t.Fatal("schedule did not use connection default", err)
	}
}
