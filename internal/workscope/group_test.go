package workscope

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestIsolationCancellationAndSelfClose(t *testing.T) {
	g, err := New(1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close(context.Background())
	for _, mode := range []string{"panic", "goexit", "close"} {
		err := g.Run(t.Context(), "test", func(ctx context.Context) error {
			switch mode {
			case "panic":
				panic("private")
			case "goexit":
				runtime.Goexit()
			case "close":
				return g.Close(ctx)
			}
			return nil
		})
		if err == nil {
			t.Fatal("invalid callback accepted")
		}
		if mode == "close" && !errors.Is(err, fault.Cycle) {
			t.Fatal("self close deadlock guard", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ran := false
	if err := g.Run(ctx, "canceled", func(context.Context) error { ran = true; return nil }); !errors.Is(err, context.Canceled) || ran {
		t.Fatal("canceled callback invoked", err)
	}
	if err := g.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := g.Run(t.Context(), "closed", func(context.Context) error { return nil }); err == nil {
		t.Fatal("closed service admitted work")
	}
}

func TestAdmissionQueuesBurstsAndReportsOverload(t *testing.T) {
	// The queued and nested probes use a long operation timeout, so neither a
	// lease deadline nor the admission wait can end them under a slow
	// scheduler; only the overload probe below uses a short wait.
	g, err := New(1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close(context.Background())
	var held *Lease
	// Release the current lease before the deferred Close on failure, so a
	// failed assertion cannot turn into a Close waiting for it forever.
	defer func() { held.Release() }()
	held, err = g.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// A queued caller is admitted once the slot is released within the wait.
	admitted := make(chan error, 1)
	go func() {
		lease, err := g.Begin(t.Context())
		if err == nil {
			lease.Release()
		}
		admitted <- err
	}()
	time.Sleep(10 * time.Millisecond)
	held.Release()
	if err := <-admitted; err != nil {
		t.Fatal("queued caller was not admitted", err)
	}
	// Nested admission never waits for capacity held by its own caller. A
	// waiting probe would block for the full admission wait, five seconds.
	held, err = g.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := g.Begin(held.Context()); !errors.Is(err, fault.Overloaded) || time.Since(started) >= time.Second {
		t.Fatal("nested admission waited or succeeded", err)
	}
	held.Release()
	// An unsatisfied wait is a retryable overload, not an internal conflict.
	short, err := New(1, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer short.Close(context.Background())
	blocking, err := short.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocking.Release()
	if _, err := short.Begin(t.Context()); !errors.Is(err, fault.Overloaded) {
		t.Fatal("expected overload after the admission wait", err)
	}
	blocking.Release()
	// Closing wakes queued callers with a closed classification.
	held, err = g.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := New(1, time.Minute)
	blocker, _ := g2.Begin(t.Context())
	waiting := make(chan error, 1)
	go func() { _, err := g2.Begin(t.Context()); waiting <- err }()
	time.Sleep(10 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- g2.Close(t.Context()) }()
	if err := <-waiting; !errors.Is(err, fault.Closed) {
		t.Fatal("close did not wake queued caller", err)
	}
	blocker.Release()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	held.Release()
}
