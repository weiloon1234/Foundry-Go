package lockout_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

type email string

func store(t *testing.T, backend lockout.Backend, configs ...lockout.Config) *lockout.Store {
	t.Helper()
	config := lockout.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "lockout"})
	if len(configs) > 0 {
		config = configs[0]
	}
	s, err := lockout.NewStore(backend, config)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func local(t *testing.T) (*memory.Backend, *testkit.Clock) {
	t.Helper()
	clock := testkit.NewClock(time.Unix(1000, 0))
	b, err := memory.New(100, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b, clock
}
func throttle(t *testing.T, s *lockout.Store, policy lockout.Policy) lockout.Throttle[email] {
	t.Helper()
	th, err := lockout.Define("password", keyspace.StringKeys[email](), policy).Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	return th
}
func TestTypedThrottleThresholdObserverAndReset(t *testing.T) {
	b, clock := local(t)
	policy := lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: 1500 * time.Millisecond}
	th := throttle(t, store(t, b), policy)
	calls, notices := 0, 0
	private := errors.New("private observer detail")
	th, err := th.WithLockedObserver(func(_ context.Context, n lockout.Notice[email]) error {
		notices++
		if n.Key() != "private@example.test" || n.RetryAfter() != policy.LockFor {
			t.Error("wrong typed notice")
		}
		encoded, err := json.Marshal(n)
		if err != nil || string(encoded) != "{}" || strings.Contains(fmt.Sprintf("%+v", n), "private") {
			t.Error("notice leaked account")
		}
		return private
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt := func(context.Context) (bool, error) { calls++; return false, nil }
	if ok, err := th.Run(t.Context(), "private@example.test", attempt); ok || err != nil {
		t.Fatal(ok, err)
	}
	ok, err := th.Run(t.Context(), "private@example.test", attempt)
	var rejection *lockout.Rejection
	if ok || !errors.Is(err, lockout.Locked) || !errors.Is(err, private) || !errors.As(err, &rejection) || rejection.RetryAfter() != policy.LockFor || strings.Contains(err.Error(), "private") {
		t.Fatal("threshold failure", err)
	}
	clock.Advance(time.Second)
	if ok, err := th.Run(t.Context(), "private@example.test", attempt); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal(ok, err)
	}
	if calls != 2 || notices != 1 {
		t.Fatal("locked attempt ran verifier or duplicated observer")
	}
	if changed, err := th.Reset(t.Context(), "private@example.test"); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if ok, err := th.Run(t.Context(), "private@example.test", func(context.Context) (bool, error) { return true, nil }); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

type wrapped struct {
	lockout.Backend
	begin  func(context.Context, lockout.Key, lockout.Policy, lockout.Generation) (lockout.Admission, error)
	finish func(context.Context, lockout.Key, lockout.Policy, lockout.Snapshot, lockout.Outcome) (lockout.Decision, error)
}

func (w wrapped) LockoutBegin(ctx context.Context, k lockout.Key, p lockout.Policy, g lockout.Generation) (lockout.Admission, error) {
	if w.begin != nil {
		return w.begin(ctx, k, p, g)
	}
	return w.Backend.LockoutBegin(ctx, k, p, g)
}
func (w wrapped) LockoutFinish(ctx context.Context, k lockout.Key, p lockout.Policy, s lockout.Snapshot, o lockout.Outcome) (lockout.Decision, error) {
	if w.finish != nil {
		return w.finish(ctx, k, p, s, o)
	}
	return w.Backend.LockoutFinish(ctx, k, p, s, o)
}
func TestBackendFailuresNeverReturnAuthorityOrRetry(t *testing.T) {
	for _, mode := range []string{"begin-error", "bad-admission", "finish-error", "bad-decision", "success-triggered-lock"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := local(t)
			begin, finish, verifies := 0, 0, 0
			private := errors.New("private backend detail")
			w := wrapped{Backend: b}
			w.begin = func(ctx context.Context, k lockout.Key, p lockout.Policy, g lockout.Generation) (lockout.Admission, error) {
				begin++
				if mode == "begin-error" {
					return lockout.Admission{}, private
				}
				if mode == "bad-admission" {
					return lockout.Admission{}, nil
				}
				return b.LockoutBegin(ctx, k, p, g)
			}
			w.finish = func(ctx context.Context, k lockout.Key, p lockout.Policy, s lockout.Snapshot, o lockout.Outcome) (lockout.Decision, error) {
				finish++
				d, err := b.LockoutFinish(ctx, k, p, s, o)
				if err != nil {
					return d, err
				}
				switch mode {
				case "finish-error":
					return d, private
				case "bad-decision":
					return lockout.Decision{}, nil
				case "success-triggered-lock":
					return lockout.Decision{Status: lockout.StatusLocked, RetryAfter: p.LockFor, Triggered: true}, nil
				}
				return d, nil
			}
			th := throttle(t, store(t, w), lockout.DefaultPolicy())
			ok, err := th.Run(t.Context(), "member", func(context.Context) (bool, error) { verifies++; return true, nil })
			if ok || !errors.Is(err, lockout.Unavailable) || strings.Contains(err.Error(), "private") {
				t.Fatal(ok, err)
			}
			expected := 1
			if strings.HasPrefix(mode, "begin") || mode == "bad-admission" {
				expected = 0
			}
			if begin != 1 || finish != expected || verifies != expected {
				t.Fatal("operation retried or verifier ran before admission", begin, finish, verifies)
			}
		})
	}
}
func TestVerifierFailureCancellationAndExpiryDoNotGrantAuthority(t *testing.T) {
	b, clock := local(t)
	th := throttle(t, store(t, b), lockout.DefaultPolicy())
	for _, mode := range []string{"panic", "goexit", "error"} {
		for range 2 {
			ok, err := th.Run(t.Context(), email(mode), func(context.Context) (bool, error) {
				switch mode {
				case "panic":
					panic("private callback")
				case "goexit":
					runtime.Goexit()
				}
				return true, errors.New("private callback")
			})
			if ok || err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(mode, ok, err)
			}
		}
		if ok, err := th.Run(t.Context(), email(mode), func(context.Context) (bool, error) { return true, nil }); err != nil || !ok {
			t.Fatal("failed callback consumed failure quota", err)
		}
	}
	ok, err := th.Run(t.Context(), "expiry", func(context.Context) (bool, error) { clock.Advance(lockout.DefaultPolicy().Window); return true, nil })
	if ok || !errors.Is(err, lockout.Expired) {
		t.Fatal("late success granted authority", ok, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ok, err = th.Run(ctx, "cancellation", func(context.Context) (bool, error) { cancel(); return true, nil })
	if ok || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled verification granted authority", ok, err)
	}
}
func TestCanceledCallbackOwnsSlotUntilActualExit(t *testing.T) {
	b, _ := local(t)
	config := lockout.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "lockout"})
	config.MaxConcurrent = 1
	config.Timeout = 100 * time.Millisecond // Also bounds the queued admission wait.
	th := throttle(t, store(t, b, config), lockout.DefaultPolicy())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		ok, err := th.Run(ctx, "member", func(context.Context) (bool, error) { close(entered); <-release; return true, nil })
		if ok {
			err = errors.New("late result accepted")
		}
		done <- err
	}()
	<-entered
	cancel()
	if ok, err := th.Run(t.Context(), "another", func(context.Context) (bool, error) { t.Error("over-capacity verifier ran"); return true, nil }); ok || !errors.Is(err, fault.Overloaded) {
		t.Error(ok, err)
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestTypedDeclarationsValidationAndBoundedKeys(t *testing.T) {
	b, _ := local(t)
	s := store(t, b)
	d := lockout.Define("password", keyspace.StringKeys[email](), lockout.DefaultPolicy())
	if _, err := d.Bind(s); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Bind(s); err != nil {
		t.Fatal("same declaration could not rebind", err)
	}
	if _, err := lockout.Define("password", keyspace.StringKeys[email](), lockout.DefaultPolicy()).Bind(s); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	config := lockout.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "small"})
	config.MaxKeyBytes = 3
	th := throttle(t, store(t, b, config), lockout.DefaultPolicy())
	if ok, err := th.Run(t.Context(), "toolong", func(context.Context) (bool, error) { t.Error("oversize key reached verifier"); return true, nil }); ok || !errors.Is(err, fault.Invalid) {
		t.Fatal(ok, err)
	}
	var missing *memory.Backend
	if _, err := lockout.NewStore(missing, config); !errors.Is(err, fault.Invalid) {
		t.Fatal("typed nil backend accepted", err)
	}
	for _, p := range []lockout.Policy{{}, {MaxFailures: 1, Window: time.Nanosecond, LockFor: time.Second}, {MaxFailures: lockout.MaxFailures + 1, Window: time.Second, LockFor: time.Second}} {
		if p.Validate() == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestStoreAndFactorNamespacesRemainIndependent(t *testing.T) {
	b, _ := local(t)
	policy := lockout.DefaultPolicy()
	policy.MaxFailures = 1
	makeThrottle := func(application, environment, name string) lockout.Throttle[email] {
		config := lockout.DefaultConfig(keyspace.Namespace{Application: application, Environment: environment})
		s := store(t, b, config)
		th, err := lockout.Define(lockout.Name(name), keyspace.StringKeys[email](), policy).Bind(s)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	locked := makeThrottle("one", "test", "password")
	if ok, err := locked.Run(t.Context(), "member", func(context.Context) (bool, error) { return false, nil }); ok || !errors.Is(err, lockout.Locked) {
		t.Fatal(ok, err)
	}
	for _, scope := range [][3]string{{"two", "test", "password"}, {"one", "other", "password"}, {"one", "test", "mfa"}} {
		if ok, err := makeThrottle(scope[0], scope[1], scope[2]).Run(t.Context(), "member", func(context.Context) (bool, error) { return true, nil }); err != nil || !ok {
			t.Fatal("cross-scope lockout", scope, err)
		}
	}
}
func TestObserverAbnormalExitDoesNotUndoCommittedLockout(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		b, _ := local(t)
		policy := lockout.DefaultPolicy()
		policy.MaxFailures = 1
		th := throttle(t, store(t, b), policy)
		calls := 0
		th, err := th.WithLockedObserver(func(context.Context, lockout.Notice[email]) error {
			calls++
			if mode == "panic" {
				panic("private observer")
			}
			runtime.Goexit()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		ok, err := th.Run(t.Context(), "member", func(context.Context) (bool, error) { return false, nil })
		if ok || !errors.Is(err, lockout.Locked) || !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private") {
			t.Fatal("observer escaped denial", ok, err)
		}
		if ok, err := th.Run(t.Context(), "member", func(context.Context) (bool, error) { t.Error("locked verifier ran"); return true, nil }); ok || !errors.Is(err, lockout.Locked) || calls != 1 {
			t.Fatal(ok, err, calls)
		}
	}
}
