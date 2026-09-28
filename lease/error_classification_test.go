package lease_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/lease/memory"
)

type renewalClassificationError struct{}

func (renewalClassificationError) Error() string { return "renewal failure" }
func (renewalClassificationError) Is(error) bool { runtime.Goexit(); return false }

func TestLeaseCallbackCleanupDoesNotInspectLostBackendCause(t *testing.T) {
	raw, err := memory.New(4)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_, leases := manager(t, wrapped{Backend: raw, renew: func(context.Context, lease.Key, lease.Owner, time.Duration) (bool, error) {
		return false, renewalClassificationError{}
	}}, nil)
	done := make(chan error, 1)
	go func() {
		_, err := leases.With(t.Context(), "lost", time.Second, 0, func(ctx context.Context) error { <-ctx.Done(); return nil })
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("lost lease was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backend error escaped lease cleanup")
	}
}
