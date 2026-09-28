package scheduling_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"foundry.test/consumer/scheduling"
	"github.com/weiloon1234/Foundry-Go"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type reportService struct{ calls chan time.Time }

func (r reportService) BuildDaily(_ context.Context, at time.Time) error { r.calls <- at; return nil }

func TestSchedulerKernelUsesConsumerServiceAndExplicitZone(t *testing.T) {
	backend, err := memory.New(64)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	manager, err := lease.NewManager(backend, lease.DefaultConfig(keyspace.Namespace{Application: "consumer", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	clock := testkit.NewClock(time.Date(2026, 9, 15, 18, 59, 0, 0, time.UTC))
	wake := make(chan struct{}, 1)
	config := schedule.DefaultConfig("reports")
	config.Clock = clock
	config.Wake = wake
	reports := reportService{calls: make(chan time.Time, 1)}
	app, err := foundry.New().Register(scheduling.Module(reports, manager, config)).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := foundation.Resolve(app.Services(), scheduling.SchedulerKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, foundation.Scheduler) }()
	deadline := time.Now().Add(3 * time.Second)
	for !scheduler.Snapshot().Leader && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !scheduler.Snapshot().Leader {
		t.Fatal("scheduler failed to acquire leadership")
	}
	clock.Advance(time.Minute)
	wake <- struct{}{}
	select {
	case at := <-reports.calls:
		if !at.Equal(time.Date(2026, 9, 15, 19, 0, 0, 0, time.UTC)) {
			t.Fatal("schedule interpreted wrong timezone")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("schedule did not invoke domain service")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler kernel did not drain")
	}
}
