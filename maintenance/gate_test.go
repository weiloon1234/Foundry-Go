package maintenance_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/maintenance"
)

func TestMaintenanceResumesWaitersAndDrainIsTerminal(t *testing.T) {
	var gate maintenance.Gate
	if gate.Admit() != nil || gate.Mode() != maintenance.Serving {
		t.Fatal("new application is paused")
	}
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(gate.Admit(), maintenance.ErrMaintenance) {
		t.Fatal("maintenance admitted work")
	}
	var waiting sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		waiting.Go(func() { results <- gate.Wait(t.Context()) })
	}
	if err := gate.Set(false); err != nil {
		t.Fatal(err)
	}
	waiting.Wait()
	for range 16 {
		if err := <-results; err != nil {
			t.Fatal("resumed waiter failed", err)
		}
	}
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	for range 16 {
		waiting.Go(func() { results <- gate.Wait(t.Context()) })
	}
	gate.Drain()
	gate.Drain()
	waiting.Wait()
	for range 16 {
		if err := <-results; !errors.Is(err, maintenance.ErrDraining) {
			t.Fatal("drain admitted a waiter", err)
		}
	}
	if !errors.Is(gate.Set(false), maintenance.ErrDraining) || !errors.Is(gate.Admit(), maintenance.ErrDraining) {
		t.Fatal("terminal drain reopened admission")
	}
}

func TestMaintenanceWaitHonorsCancellationAndOptionalGate(t *testing.T) {
	var gate maintenance.Gate
	if err := gate.Set(true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(gate.Wait(ctx), context.Canceled) {
		t.Fatal("cancelled waiter was admitted")
	}
	var optional *maintenance.Gate
	if optional.Admit() != nil || optional.Mode() != maintenance.Serving || optional.Wait(t.Context()) != nil {
		t.Fatal("optional gate changed serving behavior")
	}
}
