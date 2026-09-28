package redis

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
)

func TestRedisNamespaceContract(t *testing.T) {
	cachetest.RunNamespace(t, func(t *testing.T) cachetest.NamespaceFixture {
		client, namespace, track := integrationAddresses(t, nil)
		return cachetest.NamespaceFixture{Backend: client, Namespace: namespace, Track: func(key cache.EntryKey) { track(key.String()) }}
	})
}

func TestDistributedRememberNamespaceRotationSeparatesOwners(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "tagged"}[tagged], func(t *testing.T) {
			f := newCoordinatedFixture(t, tagged, time.Second, nil)
			entered := make(chan struct{})
			release := make(chan struct{})
			result := rememberAsync(t, f.values[0], func(ctx context.Context) (string, error) {
				close(entered)
				select {
				case <-release:
					return "old", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			})
			<-entered
			if err := f.stores[1].Invalidate(t.Context()); err != nil {
				t.Fatal(err)
			}
			value, err := f.values[1].Remember(t.Context(), "profile", cache.Forever(), func(context.Context) (string, error) { return "new", nil })
			if err != nil || value != "new" {
				t.Fatal("new generation joined old distributed lease", value, err)
			}
			close(release)
			if got := <-result; !errors.Is(got.err, fault.Conflict) || got.value != "" {
				t.Fatal("stale owner published", got)
			}
			if value, hit, err := f.values[0].Get(t.Context(), "profile"); err != nil || !hit || value != "new" {
				t.Fatal(value, hit, err)
			}
		})
	}
}

type lostNamespaceAcknowledgement struct {
	calls   *atomic.Int32
	failure error
}

func (h lostNamespaceAcknowledgement) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h lostNamespaceAcknowledgement) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h lostNamespaceAcknowledgement) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		matches := len(args) > 6 && args[0] == "eval" && args[1] == tagVersionsScript && args[6] == "1"
		err := next(ctx, cmd)
		if matches {
			h.calls.Add(1)
			if err == nil {
				return h.failure
			}
		}
		return err
	}
}
func TestNamespaceInvalidationUnknownAcknowledgementDoesNotRetry(t *testing.T) {
	f := newCoordinatedFixture(t, false, time.Second, nil)
	if err := f.values[0].Put(t.Context(), "profile", "old", cache.Forever()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	failure := errors.New("namespace acknowledgement lost")
	f.clients[0].raw.AddHook(lostNamespaceAcknowledgement{calls: &calls, failure: failure})
	if err := f.stores[0].Invalidate(t.Context()); !errors.Is(err, failure) || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	if _, hit, err := f.values[1].Get(t.Context(), "profile"); err != nil || hit {
		t.Fatal("applied rotation was rolled back", hit, err)
	}
}
