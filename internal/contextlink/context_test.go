package contextlink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
)

type operationKey struct{}

// deferredOwner queues its AfterFunc behind a test gate, making the scheduling
// window deterministic without depending on GOMAXPROCS or arbitrary sleeps.
type deferredOwner struct {
	context.Context
	allow <-chan struct{}
}

// Hide the underlying cancel context so context.AfterFunc uses this adapter.
// Operation values still come from the separate primary context passed to Link.
func (deferredOwner) Value(any) any { return nil }
func (c deferredOwner) AfterFunc(fn func()) func() bool {
	return context.AfterFunc(c.Context, func() { <-c.allow; fn() })
}

func TestLinkErrorObservesOwnerBeforeDeferredCancellationCallback(t *testing.T) {
	owner, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	allow := make(chan struct{})
	defer close(allow)
	parents := []context.Context{deferredOwner{Context: owner, allow: allow}}
	linked, release := contextlink.Link(context.WithValue(t.Context(), operationKey{}, "operation"), parents...)
	defer release()
	// Link must own the parent list, including after the caller changes its slice.
	parents[0] = context.Background()
	cancelOwner()
	if !errors.Is(linked.Err(), context.Canceled) {
		t.Fatal("queued owner cancellation was reported as successful work")
	}
	select {
	case <-linked.Done():
	default:
		t.Fatal("Err reported cancellation while Done remained open")
	}
	if linked.Value(operationKey{}) != "operation" {
		t.Fatal("synchronous cancellation changed operation values")
	}
}

func TestLinkRetainsOperationValuesAndAllOwnersCanCancel(t *testing.T) {
	for _, owner := range []string{"operation", "first", "second", "release"} {
		t.Run(owner, func(t *testing.T) {
			operation, stopOperation := context.WithCancel(context.WithValue(t.Context(), operationKey{}, "operation"))
			defer stopOperation()
			first, stopFirst := context.WithCancel(t.Context())
			defer stopFirst()
			second, stopSecond := context.WithCancel(t.Context())
			defer stopSecond()
			linked, release := contextlink.Link(operation, first, second)
			defer release()
			if linked.Value(operationKey{}) != "operation" {
				t.Fatal("link lost operation values")
			}
			switch owner {
			case "operation":
				stopOperation()
			case "first":
				stopFirst()
			case "second":
				stopSecond()
			case "release":
				release()
			}
			select {
			case <-linked.Done():
				if !errors.Is(linked.Err(), context.Canceled) {
					t.Fatal("unexpected cancellation")
				}
			case <-time.After(time.Second):
				t.Fatal("owner cancellation did not propagate")
			}
		})
	}
	canceled, stop := context.WithCancel(t.Context())
	stop()
	linked, release := contextlink.Link(t.Context(), canceled)
	defer release()
	if !errors.Is(linked.Err(), context.Canceled) {
		t.Fatal("already canceled parent was linked asynchronously")
	}
}
