package lockout_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

type privateLockoutSourceDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateLockoutSourceDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private lockout diagnostic")
}
func (e *privateLockoutSourceDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestLockoutSourceDiagnosticsSurviveCredentialContainment(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, stage := range []string{"unavailable", "locked", "expired"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", stage, exit), func(t *testing.T) {
				cause := &privateLockoutSourceDiagnostic{exit: exit}
				backend, _ := local(t)
				adapter := wrapped{Backend: backend}
				code := lockout.Locked
				switch stage {
				case "unavailable":
					code = lockout.Unavailable
					adapter.begin = func(context.Context, lockout.Key, lockout.Policy, lockout.Generation) (lockout.Admission, error) {
						return lockout.Admission{}, cause
					}
				case "expired":
					code = lockout.Expired
					adapter.finish = func(context.Context, lockout.Key, lockout.Policy, lockout.Snapshot, lockout.Outcome) (lockout.Decision, error) {
						return lockout.Decision{Status: lockout.StatusExpired}, nil
					}
				}
				policy := lockout.Policy{MaxFailures: 1, Window: time.Minute, LockFor: time.Second}
				th := throttle(t, store(t, adapter), policy)
				if stage == "locked" {
					var err error
					th, err = th.WithLockedObserver(func(context.Context, lockout.Notice[email]) error { return cause })
					if err != nil {
						t.Fatal(err)
					}
				}
				var original error
				result := callback.Isolated("login", func() error {
					allowed, err := th.Run(t.Context(), "private account", func(context.Context) (bool, error) { return false, nil })
					if allowed {
						t.Error("failed verification granted authority")
					}
					original = err
					return err
				})
				if original == nil || !errors.Is(result, original) || !errors.Is(result, code) {
					t.Fatal("lockout lost its rejection classification or identity")
				}
				if stage != "expired" {
					var retained *privateLockoutSourceDiagnostic
					if !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause {
						t.Fatal("lockout lost its private cause")
					}
				}
				if stage == "locked" {
					var rejected *lockout.Rejection
					if !errors.As(result, &rejected) || !errors.Is(result, rejected) || !rejected.Triggered() || rejected.RetryAfter() != policy.LockFor {
						t.Fatal("lockout lost typed retry metadata")
					}
				}
				want := "login: credential operation: " + string(code)
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("lockout diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("lockout formatted an application cause")
				}
			})
		}
	}
}
