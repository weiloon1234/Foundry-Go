package lease

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/weiloon1234/Foundry-Go/internal/contextlink"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type releaseOrderBackend struct{}

func (releaseOrderBackend) LeaseAcquire(context.Context, Key, Owner, time.Duration) (bool, error) {
	return true, nil
}
func (releaseOrderBackend) LeaseRenew(context.Context, Key, Owner, time.Duration) (bool, error) {
	return true, nil
}
func (releaseOrderBackend) LeaseRelease(context.Context, Key, Owner) (bool, error) { return true, nil }
func TestReleaseObservesPreviouslyCanceledManager(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, err := NewManager(releaseOrderBackend{}, DefaultConfig(keyspace.Namespace{Application: "test", Environment: "release-order"}))
		if err != nil {
			t.Fatal(err)
		}
		parent, unlink := contextlink.Link(t.Context(), m.ctx)
		key, _ := NewKey(m.config.Namespace, "test", "a")
		owner, _ := NewOwner()
		guard := newGuard(m, parent, unlink, key, owner, time.Second, time.Now().Add(time.Second), false, nil)
		// Cancellation precedes release; its AfterFunc may still be queued. It must
		// retain failure attribution rather than appear to be an ordinary release.
		m.cancel()
		if err := guard.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := guard.Err(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation was hidden by release: %v", err)
		}
		m.Close(t.Context())
	})
}
