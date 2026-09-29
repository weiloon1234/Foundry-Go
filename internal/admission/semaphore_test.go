package admission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSemaphoreWaitsThenReportsOverload(t *testing.T) {
	s := New(1)
	if err := s.Acquire(t.Context(), time.Second, nil); err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(10 * time.Millisecond); s.Release() }()
	if err := s.Acquire(t.Context(), time.Second, nil); err != nil {
		t.Fatal("waiter was not admitted after release", err)
	}
	if err := s.Acquire(t.Context(), 5*time.Millisecond, nil); !errors.Is(err, fault.Overloaded) {
		t.Fatal("expected overload", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := s.Acquire(ctx, time.Second, nil); !errors.Is(err, fault.Overloaded) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("caller deadline must end the wait as overload", err)
	}
	stop := make(chan struct{})
	close(stop)
	if err := s.Acquire(t.Context(), time.Second, stop); !errors.Is(err, fault.Closed) {
		t.Fatal("closed admission accepted a waiter", err)
	}
	if s.Active() != 1 || s.Capacity() != 1 || Wait(time.Minute) != DefaultWait || Wait(time.Second) != time.Second {
		t.Fatal("unexpected accounting")
	}
}
