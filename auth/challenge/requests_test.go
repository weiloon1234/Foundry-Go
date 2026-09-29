package challenge

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/ratelimit"
	limitmemory "github.com/weiloon1234/Foundry-Go/ratelimit/memory"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type requestIssuer struct {
	issue func(context.Context, model.Reference[member, int64]) (Issued[member, PasswordReset], error)
}

func (i *requestIssuer) Validate() error { return nil }
func (i *requestIssuer) Issue(ctx context.Context, ref model.Reference[member, int64]) (Issued[member, PasswordReset], error) {
	return i.issue(ctx, ref)
}
func requestLimiter(t *testing.T, amount uint32) ratelimit.Limiter[string] {
	t.Helper()
	memory, err := limitmemory.New(32, testkit.NewClock(time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := ratelimit.NewStore(memory, ratelimit.DefaultConfig(keyspace.Namespace{Application: "recovery-requests", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.Define("reset", keyspace.StringKeys[string](), ratelimit.PerHour(amount)).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}
func requestIssued(t *testing.T, ref model.Reference[member, int64]) Issued[member, PasswordReset] {
	t.Helper()
	token, err := ParseToken[member, PasswordReset](secret.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	if err != nil {
		t.Fatal(err)
	}
	until, err := temporal.NewDateTime(time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return Issued[member, PasswordReset]{subject: member{ID: ref.Key(), Email: "stored@example.test", Enabled: true}, token: token, expires: until}
}

type failingRequestLog struct {
	slog.Handler
	mode string
}

func (h failingRequestLog) Handle(context.Context, slog.Record) error {
	if h.mode == "panic" {
		panic("private logger value")
	}
	runtime.Goexit()
	return nil
}
func TestRecoveryRequestsAcknowledgeWithoutExposingExistenceOrFailures(t *testing.T) {
	for _, mode := range []string{"delivered", "absent", "ineligible", "lookup-error", "issuer-error", "wrong-subject", "empty-token", "delivery-error", "delivery-panic", "delivery-goexit", "logger-panic", "logger-goexit", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var log bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&log, nil))
			if strings.HasPrefix(mode, "logger-") {
				logger = slog.New(failingRequestLog{Handler: logger.Handler(), mode: strings.TrimPrefix(mode, "logger-")})
			}
			lookupCalls, issueCalls, deliverCalls := 0, 0, 0
			issuer := &requestIssuer{issue: func(ctx context.Context, ref model.Reference[member, int64]) (Issued[member, PasswordReset], error) {
				issueCalls++
				if mode == "ineligible" {
					return Issued[member, PasswordReset]{}, auth.Unauthenticated
				}
				if mode == "issuer-error" {
					return Issued[member, PasswordReset]{}, errors.New("private issuer error")
				}
				issued := requestIssued(t, ref)
				if mode == "wrong-subject" {
					issued.subject.ID++
				}
				if mode == "empty-token" {
					issued.token = Token[member, PasswordReset]{}
				}
				return issued, nil
			}}
			callbacks := RequestCallbacks[member, int64, string, PasswordReset]{
				Lookup: func(_ context.Context, input string) (value.Optional[model.Reference[member, int64]], error) {
					lookupCalls++
					if input != "submitted@example.test" {
						t.Error("lookup input changed")
					}
					if mode == "lookup-error" {
						return value.Optional[model.Reference[member, int64]]{}, errors.New("private lookup error")
					}
					if mode == "absent" {
						return value.Optional[model.Reference[member, int64]]{}, nil
					}
					return value.Set(member{ID: 7}.reference()), nil
				},
				Deliver: func(ctx context.Context, issued Issued[member, PasswordReset]) error {
					deliverCalls++
					if issued.Subject().Email != "stored@example.test" || issued.Token().Validate() != nil {
						t.Error("delivery lost stored address or token")
					}
					switch mode {
					case "delivery-error", "logger-panic", "logger-goexit":
						return errors.New("private delivery error")
					case "delivery-panic":
						panic("private delivery panic")
					case "delivery-goexit":
						runtime.Goexit()
					case "timeout":
						<-ctx.Done()
						return ctx.Err()
					}
					return nil
				},
			}
			config := auth.DefaultConfig()
			if mode == "timeout" {
				config.Timeout = 10 * time.Millisecond
			}
			requests, err := NewRequests(issuer, requestLimiter(t, 1), callbacks, logger, config)
			if err != nil {
				t.Fatal(err)
			}
			if err := requests.Request(t.Context(), "submitted@example.test"); err != nil {
				t.Fatal("admitted request revealed outcome", err)
			}
			if err := requests.Wait(t.Context()); err != nil { // Dispatch is owned background work.
				t.Fatal(err)
			}
			if lookupCalls != 1 {
				t.Fatal("lookup count", lookupCalls)
			}
			if mode == "absent" && issueCalls != 0 {
				t.Fatal("missing recipient issued a token")
			}
			if (mode == "ineligible" || mode == "wrong-subject" || mode == "empty-token" || mode == "issuer-error" || mode == "lookup-error") && deliverCalls != 0 {
				t.Fatal("invalid issuance reached delivery")
			}
			if err := requests.Request(t.Context(), "submitted@example.test"); err != nil || lookupCalls != 1 {
				t.Fatal("denied quota was not silent or repeated lookup", err)
			}
			text := log.String()
			for _, private := range []string{"submitted@example.test", "stored@example.test", "private", "AAAAAAAA"} {
				if strings.Contains(text, private) {
					t.Fatal("diagnostics disclosed private details")
				}
			}
			if strings.Contains(mode, "error") && text == "" {
				t.Fatal("operational failure was not diagnosed")
			}
			if err := requests.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Delivery no longer runs in the caller's request, so response latency does not
// depend on account existence. The dispatch is still owned: capacity is held
// until the callback exits, the caller's cancellation does not abandon it, and
// Close cancels and drains it.
func TestRecoveryRequestDispatchIsOwnedBoundedAndDrained(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls, lookups atomic.Int32
	callbacks := RequestCallbacks[member, int64, string, PasswordReset]{
		Lookup: func(context.Context, string) (value.Optional[model.Reference[member, int64]], error) {
			lookups.Add(1)
			return value.Set(member{ID: 7}.reference()), nil
		},
		Deliver: func(context.Context, Issued[member, PasswordReset]) error {
			calls.Add(1)
			close(started)
			<-release
			return nil
		},
	}
	issuer := &requestIssuer{issue: func(_ context.Context, ref model.Reference[member, int64]) (Issued[member, PasswordReset], error) {
		return requestIssued(t, ref), nil
	}}
	config := auth.DefaultConfig()
	config.MaxConcurrent = 1
	config.Timeout = 50 * time.Millisecond
	requests, err := NewRequests(issuer, requestLimiter(t, 10), callbacks, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := requests.Request(ctx, "first@example.test"); err != nil {
		t.Fatal("request waited for account-dependent work", err)
	}
	<-started
	cancel()
	if err := requests.Request(t.Context(), "second@example.test"); !errors.Is(err, fault.Overloaded) || lookups.Load() != 1 {
		t.Fatal("dispatch capacity was released before exit", err)
	}
	closing, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer stop()
	if err := requests.Close(closing); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close did not wait for owned dispatch", err)
	}
	close(release)
	if err := requests.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := requests.Request(t.Context(), "third@example.test"); !errors.Is(err, fault.Closed) {
		t.Fatal("closed requester accepted work", err)
	}
	if calls.Load() != 1 {
		t.Fatal("delivery was retried")
	}
}

// Pre-admission failures must never be confused with account-dependent outcomes.
type requestRateBackend struct {
	fail  func(context.Context) error
	calls int
}

func (b *requestRateBackend) RateLimit(ctx context.Context, _ ratelimit.Key, _ ratelimit.Limit, _ uint32) (ratelimit.Decision, error) {
	b.calls++
	return ratelimit.Decision{}, b.fail(ctx)
}
func TestRecoveryRequestsFailBeforeLookupWhenAdmissionUnavailable(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "timeout", "canceled", "nil-context"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("private quota authority failure")
			backend := &requestRateBackend{fail: func(ctx context.Context) error {
				switch mode {
				case "panic":
					panic("private quota authority failure")
				case "goexit":
					runtime.Goexit()
				case "timeout":
					<-ctx.Done()
					return ctx.Err()
				}
				return cause
			}}
			store, err := ratelimit.NewStore(backend, ratelimit.DefaultConfig(keyspace.Namespace{Application: "recovery-requests", Environment: "test"}))
			if err != nil {
				t.Fatal(err)
			}
			limiter, err := ratelimit.Define("reset", keyspace.StringKeys[string](), ratelimit.PerHour(1)).Bind(store)
			if err != nil {
				t.Fatal(err)
			}
			callbacks := RequestCallbacks[member, int64, string, PasswordReset]{
				Lookup: func(context.Context, string) (value.Optional[model.Reference[member, int64]], error) {
					t.Error("admission failure reached account lookup")
					return value.Optional[model.Reference[member, int64]]{}, nil
				},
				Deliver: func(context.Context, Issued[member, PasswordReset]) error { t.Error("unexpected delivery"); return nil },
			}
			var logs bytes.Buffer
			config := auth.DefaultConfig()
			config.Timeout = 10 * time.Millisecond
			requests, err := NewRequests(&requestIssuer{}, limiter, callbacks, slog.New(slog.NewTextHandler(&logs, nil)), config)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if mode == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "nil-context" {
				ctx = nil
			}
			err = requests.Request(ctx, "private@example.test")
			if err == nil {
				t.Fatal("admission failure acknowledged")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("lost authority error")
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("lost deadline", err)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if (mode == "canceled" || mode == "nil-context") && backend.calls != 0 {
				t.Fatal("invalid context touched authority")
			}
			if backend.calls > 1 {
				t.Fatal("uncertain quota consumption retried")
			}
			if logs.Len() != 0 {
				t.Fatal("admission error logged as account dispatch failure")
			}
		})
	}
}
func TestRecoveryRequestsRejectIncompleteAssembly(t *testing.T) {
	issuer := &requestIssuer{}
	limiter := requestLimiter(t, 1)
	callbacks := RequestCallbacks[member, int64, string, PasswordReset]{
		Lookup: func(context.Context, string) (value.Optional[model.Reference[member, int64]], error) {
			return value.Optional[model.Reference[member, int64]]{}, nil
		},
		Deliver: func(context.Context, Issued[member, PasswordReset]) error { return nil },
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	for _, mode := range []string{"nil-issuer", "typed-nil-issuer", "limiter", "lookup", "delivery", "logger", "limits"} {
		t.Run(mode, func(t *testing.T) {
			var selected Issuer[member, int64, PasswordReset] = issuer
			bound, functions, diagnostics, config := limiter, callbacks, logger, auth.DefaultConfig()
			switch mode {
			case "nil-issuer":
				selected = nil
			case "typed-nil-issuer":
				selected = (*requestIssuer)(nil)
			case "limiter":
				bound = ratelimit.Limiter[string]{}
			case "lookup":
				functions.Lookup = nil
			case "delivery":
				functions.Deliver = nil
			case "logger":
				diagnostics = nil
			case "limits":
				config.MaxConcurrent = 0
			}
			if _, err := NewRequests(selected, bound, functions, diagnostics, config); err == nil {
				t.Fatal("invalid requester accepted")
			}
		})
	}
	var missing *Requests[member, int64, string, PasswordReset]
	if err := missing.Request(t.Context(), "input"); err == nil {
		t.Fatal("nil requester accepted")
	}
}

type cyclicRecoveryIssuerError struct{ visits atomic.Int32 }

func (*cyclicRecoveryIssuerError) Error() string {
	panic("private recovery error must not be formatted")
}
func (e *cyclicRecoveryIssuerError) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}
func TestCyclicRecoveryIssuerFailureIsAcknowledgedAndCapacityIsReleased(t *testing.T) {
	for _, denied := range []bool{false, true} {
		cycle := new(cyclicRecoveryIssuerError)
		var failure error = cycle
		if denied {
			failure = errors.Join(auth.Unauthenticated, cycle)
		}
		calls, delivered := 0, 0
		issuer := &requestIssuer{issue: func(_ context.Context, reference model.Reference[member, int64]) (Issued[member, PasswordReset], error) {
			calls++
			if failure != nil {
				return Issued[member, PasswordReset]{}, failure
			}
			return requestIssued(t, reference), nil
		}}
		callbacks := RequestCallbacks[member, int64, string, PasswordReset]{
			Lookup: func(context.Context, string) (value.Optional[model.Reference[member, int64]], error) {
				return value.Set(member{ID: 7}.reference()), nil
			},
			Deliver: func(context.Context, Issued[member, PasswordReset]) error { delivered++; return nil },
		}
		config := auth.DefaultConfig()
		config.MaxConcurrent = 1
		var logs bytes.Buffer
		requests, err := NewRequests(issuer, requestLimiter(t, 10), callbacks, slog.New(slog.NewTextHandler(&logs, nil)), config)
		if err != nil {
			t.Fatal(err)
		}
		if err := requests.Request(t.Context(), "submitted@example.test"); err != nil {
			t.Fatal("issuer failure exposed an account-dependent outcome")
		}
		if err := requests.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		if calls != 1 || delivered != 0 || cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
			t.Fatal("cyclic issuance failed to finish without delivery")
		}
		if !strings.Contains(logs.String(), "account recovery request failed") || strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "submitted@example.test") {
			t.Fatal("incomplete outcome was hidden or disclosed private details")
		}
		failure = nil
		err = requests.Request(t.Context(), "submitted@example.test")
		if waited := requests.Wait(t.Context()); waited != nil {
			t.Fatal(waited)
		}
		if err != nil || calls != 2 || delivered != 1 {
			t.Fatal("issuer error retained request capacity")
		}
	}
}
