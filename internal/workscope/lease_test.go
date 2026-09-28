package workscope

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLeaseRetainsCapacityAfterCancellationUntilActualRelease(t *testing.T) {
	g, err := New(1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	lease, err := g.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	cancel()
	if !errors.Is(lease.Context().Err(), context.Canceled) {
		t.Fatal("lease did not inherit cancellation")
	}
	if next, err := g.Begin(t.Context()); !errors.Is(err, fault.Conflict) || next != nil {
		t.Fatal("cancellation released actual ownership", err)
	}
	stopped, stop := context.WithCancel(t.Context())
	stop()
	if err := g.Close(stopped); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-g.Done():
		t.Fatal("shutdown completed before resource release")
	default:
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(lease.Release)
	}
	wg.Wait()
	if err := g.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.Done():
	default:
		t.Fatal("release did not complete shutdown")
	}
	if _, err := g.Begin(t.Context()); !errors.Is(err, fault.Conflict) {
		t.Fatal("closed admission reopened", err)
	}
}

func TestLeaseClosePreflightDoesNotPartiallyCancelGroups(t *testing.T) {
	first, _ := New(2, time.Minute)
	second, _ := New(1, time.Minute)
	defer first.Close(context.Background())
	defer second.Close(context.Background())
	outer, err := first.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Release()
	inner, err := second.Begin(outer.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Release()
	for _, group := range []*Group{first, second} {
		if err := group.CheckClose(inner.Context()); !errors.Is(err, fault.Cycle) {
			t.Fatal("nested ownership was not detected", err)
		}
		if err := group.Close(inner.Context()); !errors.Is(err, fault.Cycle) {
			t.Fatal("self-close proceeded", err)
		}
	}
	if outer.Context().Err() != nil || inner.Context().Err() != nil {
		t.Fatal("preflight canceled owned work")
	}
	inner.Release()
	if err := second.Close(inner.Context()); err != nil {
		t.Fatal("released frame remained active", err)
	}
	if first.CheckClose(inner.Context()) == nil {
		t.Fatal("released child hid active parent")
	}
	outer.Release()
	if err := first.Close(inner.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseContextObservesOwnerShutdownBeforeReturningToCaller(t *testing.T) {
	for range 100 {
		group, err := New(1, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := group.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		stopped, cancel := context.WithCancel(t.Context())
		cancel()
		err = group.Close(stopped)
		cancellation := lease.Context().Err()
		lease.Release()
		if !errors.Is(err, context.Canceled) || !errors.Is(cancellation, context.Canceled) {
			t.Fatal("shutdown returned before the lease observed cancellation", err, cancellation)
		}
	}
}

func TestLeaseInvalidAdmissionAndRunUseOneCapacityLimit(t *testing.T) {
	g, _ := New(1, time.Minute)
	defer g.Close(context.Background())
	var zero Group
	for _, group := range []*Group{nil, &zero} {
		if lease, err := group.Begin(t.Context()); lease != nil || !errors.Is(err, fault.Invalid) {
			t.Fatal("undefined group admitted a lease", err)
		}
	}
	if _, err := g.Begin(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := g.Run(t.Context(), "outer", func(ctx context.Context) error {
		if lease, err := g.Begin(ctx); lease != nil || !errors.Is(err, fault.Conflict) {
			t.Error("Run and Begin did not share capacity", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := g.Begin(t.Context())
	if err != nil {
		t.Fatal("Run did not release its lease", err)
	}
	lease.Release()
	var missing *Lease
	missing.Release()
	new(Lease).Release()
}
