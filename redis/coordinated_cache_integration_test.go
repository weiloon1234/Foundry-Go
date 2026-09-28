package redis

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
	"github.com/weiloon1234/Foundry-Go/lease"
)

var coordinatedValues = cache.Define("coordinated-values", cache.StringKeys[string](), cache.JSON[string]())
var coordinatedTags = cache.DefineTag("coordinated-tags", cache.StringKeys[string]())

type observedPublisher struct {
	*Client
	track    func(string)
	acquired chan bool
	before   func(context.Context, lease.Proof) error
}

func (p *observedPublisher) LeaseAcquire(ctx context.Context, key lease.Key, owner lease.Owner, ttl time.Duration) (bool, error) {
	p.track(key.String())
	ok, err := p.Client.LeaseAcquire(ctx, key, owner, ttl)
	select {
	case p.acquired <- ok:
	default:
	}
	return ok, err
}
func (p *observedPublisher) PutLeased(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL, proof lease.Proof) error {
	if p.before != nil {
		if err := p.before(ctx, proof); err != nil {
			return err
		}
	}
	return p.Client.PutLeased(ctx, key, data, ttl, proof)
}
func (p *observedPublisher) PutTaggedLeased(ctx context.Context, key cache.TaggedKey, data []byte, ttl cache.TTL, proof lease.Proof) error {
	if p.before != nil {
		if err := p.before(ctx, proof); err != nil {
			return err
		}
	}
	return p.Client.PutTaggedLeased(ctx, key, data, ttl, proof)
}

type coordinatedFixture struct {
	track      func(string)
	clients    [2]*Client
	publishers [2]*observedPublisher
	managers   [2]*lease.Manager
	values     [2]cache.Cache[string, string]
	stores     [2]*cache.Store
	tags       [2]cache.Tags[string]
	base       cache.EntryKey
	tagged     cache.TaggedKey
}

func newCoordinatedFixture(t *testing.T, tagged bool, wait time.Duration, before func(context.Context, lease.Proof) error) coordinatedFixture {
	t.Helper()
	var f coordinatedFixture
	first, namespace, track := integrationAddresses(t, nil)
	f.track = track
	second, err := Open(t.Context(), integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close(context.Background()) })
	f.clients = [2]*Client{first, second}
	base, err := cache.NewEntryKey(namespace, coordinatedValues.Name(), "profile")
	if err != nil {
		t.Fatal(err)
	}
	f.base = base
	track(base.String())
	scope, err := cache.NewNamespaceTagKey(namespace)
	if err != nil {
		t.Fatal(err)
	}
	track(scope.String())
	stampKeys := []cache.EntryKey{scope}
	if tagged {
		tagKey, err := cache.NewEntryKey(namespace, coordinatedTags.Name(), "member")
		if err != nil {
			t.Fatal(err)
		}
		track(tagKey.String())
		stampKeys = append(stampKeys, tagKey)
	}
	snapshotFixture := cachetest.TaggedFixture{Backend: first, Track: func(key cache.EntryKey) { track(key.String()) }}
	f.tagged = snapshotFixture.Snapshot(t, base, stampKeys...)
	for i, client := range f.clients {
		publisher := &observedPublisher{Client: client, track: track, acquired: make(chan bool, 32)}
		if i == 0 {
			publisher.before = before
		}
		f.publishers[i] = publisher
		m, err := lease.NewManager(publisher, lease.DefaultConfig(namespace))
		if err != nil {
			t.Fatal(err)
		}
		f.managers[i] = m
		t.Cleanup(func() { m.Close(context.Background()) })
		coordination := cache.CoordinationConfig{LeaseDuration: time.Minute, Wait: wait}
		store, err := cache.NewCoordinatedStore(m, cache.DefaultConfig(namespace), coordination)
		if err != nil {
			t.Fatal(err)
		}
		f.stores[i] = store
		values, err := coordinatedValues.Bind(store)
		if err != nil {
			t.Fatal(err)
		}
		if tagged {
			tags, err := coordinatedTags.Bind(store)
			if err != nil {
				t.Fatal(err)
			}
			f.tags[i] = tags
			tag := tags.For("member")
			values, err = values.WithTags(tag)
			if err != nil {
				t.Fatal(err)
			}
		}
		f.values[i] = values
	}
	return f
}

type rememberResult struct {
	value string
	err   error
}

func rememberAsync(t *testing.T, values cache.Cache[string, string], load func(context.Context) (string, error)) <-chan rememberResult {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan rememberResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		value, err := values.Remember(ctx, "profile", cache.For(time.Minute), load)
		result <- rememberResult{value, err}
	}()
	t.Cleanup(func() { cancel(); <-done })
	return result
}
func awaitAcquisition(t *testing.T, events <-chan bool) bool {
	t.Helper()
	select {
	case ok := <-events:
		return ok
	case <-time.After(5 * time.Second):
		t.Fatal("acquisition did not reach authority")
		return false
	}
}
func TestDistributedRememberCoalescesAcrossClients(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		name := "plain"
		if tagged {
			name = "tagged"
		}
		t.Run(name, func(t *testing.T) {
			f := newCoordinatedFixture(t, tagged, time.Second, nil)
			entered := make(chan struct{})
			finish := make(chan struct{})
			var calls atomic.Int32
			first := rememberAsync(t, f.values[0], func(ctx context.Context) (string, error) {
				calls.Add(1)
				close(entered)
				select {
				case <-finish:
					return "shared", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			})
			<-entered
			second := rememberAsync(t, f.values[1], func(context.Context) (string, error) { calls.Add(1); return "duplicate", nil })
			if awaitAcquisition(t, f.publishers[1].acquired) {
				t.Fatal("both clients acquired")
			}
			close(finish)
			for _, result := range []<-chan rememberResult{first, second} {
				got := <-result
				if got.err != nil || got.value != "shared" {
					t.Fatal(got)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("duplicate loader", calls.Load())
			}
			for _, m := range f.managers {
				if m.Stats().Active != 0 {
					t.Fatal("scope did not drain", m.Stats())
				}
			}
		})
	}
}
func TestDistributedRememberRejectsSupersededPublication(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		name := "plain"
		if tagged {
			name = "tagged"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan lease.Proof, 1)
			finish := make(chan struct{})
			f := newCoordinatedFixture(t, tagged, time.Second, func(ctx context.Context, proof lease.Proof) error {
				entered <- proof
				select {
				case <-finish:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			old := rememberAsync(t, f.values[0], func(context.Context) (string, error) { return "old", nil })
			proof := <-entered
			if err := f.clients[1].raw.PExpire(t.Context(), proof.Key().String(), 0).Err(); err != nil {
				t.Fatal(err)
			}
			value, err := f.values[1].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) { return "new", nil })
			if err != nil || value != "new" {
				t.Fatal(value, err)
			}
			// The old client's local validity remains live; only an atomic authority check
			// can reject this publication after the successor has finished and released.
			if err := proof.Validate(); err != nil {
				t.Fatal("test lost its remote-only loss condition", err)
			}
			close(finish)
			result := <-old
			if result.value != "" || !errors.Is(result.err, lease.ErrLost) {
				t.Fatal(result)
			}
			value, found, err := f.values[1].Get(t.Context(), "profile")
			if err != nil || !found || value != "new" {
				t.Fatal("stale owner replaced value", value, found, err)
			}
		})
	}
}
func TestDistributedRememberWaitTimeoutDoesNotRunFallback(t *testing.T) {
	f := newCoordinatedFixture(t, false, 100*time.Millisecond, nil)
	entered := make(chan struct{})
	finish := make(chan struct{})
	first := rememberAsync(t, f.values[0], func(ctx context.Context) (string, error) {
		close(entered)
		select {
		case <-finish:
			return "owner", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	<-entered
	var calls atomic.Int32
	value, err := f.values[1].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) { calls.Add(1); return "fallback", nil })
	if value != "" || !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 0 {
		t.Fatal(value, err, calls.Load())
	}
	close(finish)
	if result := <-first; result.err != nil || result.value != "owner" {
		t.Fatal(result)
	}
}
func TestDistributedRememberTagInvalidationDuringLoad(t *testing.T) {
	entered := make(chan lease.Proof, 1)
	finish := make(chan struct{})
	f := newCoordinatedFixture(t, true, time.Second, func(ctx context.Context, proof lease.Proof) error {
		entered <- proof
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	old := rememberAsync(t, f.values[0], func(context.Context) (string, error) { return "old-tag", nil })
	<-entered
	if err := f.tags[1].Invalidate(t.Context(), "member"); err != nil {
		t.Fatal(err)
	}
	value, err := f.values[1].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) { return "new-tag", nil })
	if err != nil || value != "new-tag" {
		t.Fatal(value, err)
	}
	close(finish)
	if result := <-old; result.value != "" || !errors.Is(result.err, fault.Conflict) {
		t.Fatal(result)
	}
	if value, found, err := f.values[1].Get(t.Context(), "profile"); err != nil || !found || value != "new-tag" {
		t.Fatal(value, found, err)
	}
}
func TestDistributedRememberFailuresReleaseOwnership(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			f := newCoordinatedFixture(t, false, time.Second, nil)
			failure := errors.New("loader failure")
			value, err := f.values[0].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) {
				switch mode {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				}
				return "", failure
			})
			want := error(failure)
			if mode != "error" {
				want = fault.Panicked
			}
			if value != "" || !errors.Is(err, want) {
				t.Fatal(value, err)
			}
			value, err = f.values[1].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) { return "recovered", nil })
			if err != nil || value != "recovered" {
				t.Fatal(value, err)
			}
		})
	}
}
func TestDistributedRememberOwnerShutdownAndRecursiveContext(t *testing.T) {
	t.Run("shutdown", func(t *testing.T) {
		f := newCoordinatedFixture(t, false, time.Second, nil)
		entered := make(chan struct{})
		result := rememberAsync(t, f.values[0], func(ctx context.Context) (string, error) { close(entered); <-ctx.Done(); return "", ctx.Err() })
		<-entered
		if err := f.managers[0].Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := <-result; got.value != "" || !errors.Is(got.err, context.Canceled) {
			t.Fatal(got)
		}
		if value, err := f.values[1].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) { return "next", nil }); err != nil || value != "next" {
			t.Fatal(value, err)
		}
	})
	t.Run("cross-store-cycle", func(t *testing.T) {
		f := newCoordinatedFixture(t, false, time.Second, nil)
		value, err := f.values[0].Remember(t.Context(), "profile", cache.For(time.Minute), func(ctx context.Context) (string, error) {
			return f.values[1].Remember(ctx, "profile", cache.For(time.Minute), func(context.Context) (string, error) { return "unreachable", nil })
		})
		if value != "" || !errors.Is(err, fault.Cycle) {
			t.Fatal(value, err)
		}
	})
}

// The authority really applies the mutation; only its acknowledgement is hidden.
// No shared Redis configuration or ACL is changed by this fault fixture.
type lostFillAcknowledgement struct {
	operation string
	failure   error
	calls     *atomic.Int32
}

func (h lostFillAcknowledgement) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h lostFillAcknowledgement) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h lostFillAcknowledgement) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		matches := false
		if len(args) > 4 && args[0] == "eval" {
			matches = h.operation == "write" && (args[1] == cacheLeasedScript || args[1] == taggedLeasedScript) || h.operation == "release" && args[1] == leaseScript && args[4] == "release"
		}
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
func TestDistributedRememberAcknowledgementFailureIsNotSuccessOrRetry(t *testing.T) {
	for _, operation := range []string{"write", "release"} {
		t.Run(operation, func(t *testing.T) {
			f := newCoordinatedFixture(t, true, time.Second, nil)
			failure := errors.New("acknowledgement lost")
			var calls atomic.Int32
			f.clients[0].raw.AddHook(lostFillAcknowledgement{operation: operation, failure: failure, calls: &calls})
			value, err := f.values[0].Remember(t.Context(), "profile", cache.For(time.Minute), func(context.Context) (string, error) { return "applied", nil })
			if value != "" || !errors.Is(err, failure) || calls.Load() != 1 {
				t.Fatal(value, err, calls.Load())
			}
			value, found, err := f.values[1].Get(t.Context(), "profile")
			if err != nil || !found || value != "applied" {
				t.Fatal("uncertain write was incorrectly rolled back", value, found, err)
			}
			if operation == "release" {
				if err := f.managers[0].Close(t.Context()); !errors.Is(err, failure) {
					t.Fatal("cleanup error was lost", err)
				}
			}
		})
	}
}
func TestCoordinatedPublicationRequiresExactFillProof(t *testing.T) {
	entered := make(chan lease.Proof, 1)
	finish := make(chan struct{})
	f := newCoordinatedFixture(t, false, time.Second, func(ctx context.Context, proof lease.Proof) error {
		entered <- proof
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	result := rememberAsync(t, f.values[0], func(context.Context) (string, error) { return "intended", nil })
	proof := <-entered
	wrong, err := cache.NewEntryKey(f.base.Namespace(), "different-family", "profile")
	if err != nil {
		t.Fatal(err)
	}
	f.track(wrong.String())
	if err := f.clients[1].PutLeased(t.Context(), wrong, []byte("wrong"), cache.Forever(), proof); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := f.clients[1].PutLeased(nil, f.base, nil, cache.Forever(), proof); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := f.clients[1].PutLeased(t.Context(), f.base, nil, cache.Forever(), lease.Proof{}); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := cache.ValidateFillProof(t.Context(), cache.EntryKey{}, proof); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	close(finish)
	if got := <-result; got.err != nil || got.value != "intended" {
		t.Fatal(got)
	}
	if err := f.clients[1].PutLeased(t.Context(), f.base, []byte("late"), cache.Forever(), proof); !errors.Is(err, lease.ErrReleased) {
		t.Fatal("closed proof was accepted", err)
	}
}
