package auth_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type accountKey int64
type account struct {
	ID         accountKey
	Enabled    bool
	Permission bool
}

func (a account) FoundryReference() model.Reference[account, accountKey] {
	return model.NewReference[account]("accounts", a.ID, codec.Signed[accountKey]())
}
func (a account) FoundryIdentity() (model.Identity, error) { return a.FoundryReference().Identity() }
func (account) AccessID() string                           { panic("presentation getter must not run") }

type document struct{ Owner accountKey }

func provider(load func(context.Context, accountKey) (value.Optional[account], error)) auth.Provider[account, accountKey] {
	return auth.DefineProvider("accounts", (account{}).FoundryReference(), load, func(_ context.Context, a account) (bool, error) { return a.Enabled, nil })
}
func proof(t *testing.T, key accountKey, state auth.Assurance) auth.Proof[account, accountKey] {
	t.Helper()
	p, err := auth.NewProof(account{ID: key}.FoundryReference(), state)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func strategy(t *testing.T, source auth.CredentialName, key accountKey) auth.Strategy[account, accountKey] {
	t.Helper()
	p := proof(t, key, auth.Authenticated)
	return auth.DefineStrategy(source, func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		if credential.Reveal() != "valid" {
			return value.Optional[auth.Proof[account, accountKey]]{}, nil
		}
		return value.Set(p), nil
	})
}
func inputs(t *testing.T, items ...auth.Credential) auth.Credentials {
	t.Helper()
	c, err := auth.NewCredentials(items...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func registry(t *testing.T, items ...auth.Registration) *auth.Registry {
	t.Helper()
	r, err := auth.NewRegistry(auth.DefaultConfig(), items...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func scope(t *testing.T, r *auth.Registry, items ...auth.Credential) *auth.Scope {
	t.Helper()
	s, err := r.NewScope(t.Context(), inputs(t, items...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestGuardLoadsOncePerScopeAndRefreshesCurrentModel(t *testing.T) {
	var loads atomic.Int32
	state := account{ID: 7, Enabled: true, Permission: true}
	p := provider(func(_ context.Context, key accountKey) (value.Optional[account], error) {
		loads.Add(1)
		if key != state.ID {
			t.Error("wrong typed key")
		}
		return value.Set(state), nil
	})
	api := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	web := auth.DefineGuard("web", p, strategy(t, "session", 7))
	canRead := auth.DefinePolicy("document.read", func(_ context.Context, a account, d document) (bool, error) {
		return a.Permission && a.ID == d.Owner, nil
	})
	r := registry(t, api.Registration(), web.Registration(), canRead.Registration())
	s := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")}, auth.Credential{Name: "session", Secret: secret.New("valid")})
	ctx := s.Context()
	for range 4 {
		a, err := api.Require(ctx)
		if err != nil || a.ID != 7 {
			t.Fatal(a, err)
		}
		if err := canRead.Authorize(ctx, api, document{7}); err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 {
		t.Fatal("duplicate model hydration", loads.Load())
	}
	if _, err := web.Require(ctx); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 {
		t.Fatal("guards shared authorization state")
	}
	state.Permission = false
	fresh := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if err := canRead.Authorize(fresh.Context(), api, document{7}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("permission removal was ignored", err)
	}
	state.Enabled = false
	disabled := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if got, err := api.Require(disabled.Context()); !errors.Is(err, auth.Unauthenticated) || got != (account{}) {
		t.Fatal("disabled model authenticated", got, err)
	}
	if got, err := api.Require(context.Background()); !errors.Is(err, fault.Missing) || got != (account{}) {
		t.Fatal("scope-free model accepted", got, err)
	}
}

func TestAbsentInvalidAndPendingCredentialsDoNotBecomeEquivalent(t *testing.T) {
	var loads atomic.Int32
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		loads.Add(1)
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	for _, mode := range []string{"absent", "invalid", "pending", "zero-proof", "expired-error"} {
		t.Run(mode, func(t *testing.T) {
			verified := proof(t, 7, auth.Authenticated)
			if mode == "pending" {
				verified = proof(t, 7, auth.PendingMFA)
			}
			if mode == "zero-proof" {
				verified = auth.Proof[account, accountKey]{}
			}
			var verifies atomic.Int32
			st := auth.DefineStrategy("input", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
				verifies.Add(1)
				if mode == "invalid" {
					return value.Optional[auth.Proof[account, accountKey]]{}, nil
				}
				if mode == "expired-error" {
					return value.Set(verified), auth.Unauthenticated.WithCause(errors.New("secret-expired-record"))
				}
				return value.Set(verified), nil
			})
			g := auth.DefineGuard("guard", p, st)
			r := registry(t, g.Registration())
			var c []auth.Credential
			if mode != "absent" {
				c = []auth.Credential{{Name: "input", Secret: secret.New("private-credential")}}
			}
			s := scope(t, r, c...)
			for range 2 {
				got, err := g.Optional(s.Context())
				if got.IsSet() {
					t.Fatal("unexpected subject")
				}
				switch mode {
				case "absent":
					if err != nil {
						t.Fatal(err)
					}
				case "pending":
					if !errors.Is(err, auth.MFARequired) {
						t.Fatal(err)
					}
				case "zero-proof":
					if !errors.Is(err, fault.Invalid) {
						t.Fatal(err)
					}
				default:
					if !errors.Is(err, auth.Unauthenticated) {
						t.Fatal(err)
					}
				}
				if err != nil && strings.Contains(fmt.Sprintf("%+v", err), "secret") {
					t.Fatal("credential detail leaked")
				}
			}
			want := int32(1)
			if mode == "absent" {
				want = 0
			}
			if verifies.Load() != want {
				t.Fatal("unexpected verification count", verifies.Load())
			}
			if _, err := g.Require(s.Context()); err == nil {
				t.Fatal("required authentication accepted failure")
			}
		})
	}
	if loads.Load() != 0 {
		t.Fatal("invalid/pending authentication loaded a normal model")
	}
}

func TestLookupFailuresNeverPublishPartialSubjects(t *testing.T) {
	cause := errors.New("private database credential")
	for _, mode := range []string{"absent", "wrong-key", "lookup-error", "partial-error", "lookup-panic", "lookup-goexit", "eligibility-error", "eligibility-panic"} {
		t.Run(mode, func(t *testing.T) {
			p := auth.DefineProvider("accounts", (account{}).FoundryReference(), func(context.Context, accountKey) (value.Optional[account], error) {
				switch mode {
				case "absent":
					return value.Optional[account]{}, nil
				case "wrong-key":
					return value.Set(account{ID: 8, Enabled: true}), nil
				case "lookup-error":
					return value.Optional[account]{}, cause
				case "partial-error":
					return value.Set(account{ID: 7, Enabled: true}), cause
				case "lookup-panic":
					panic("private callback secret")
				case "lookup-goexit":
					runtime.Goexit()
				}
				return value.Set(account{ID: 7, Enabled: true}), nil
			}, func(context.Context, account) (bool, error) {
				if mode == "eligibility-error" {
					return true, cause
				}
				if mode == "eligibility-panic" {
					panic("private eligibility secret")
				}
				return true, nil
			})
			g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
			s := scope(t, registry(t, g.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
			got, err := g.Require(s.Context())
			if got != (account{}) || err == nil {
				t.Fatal("partial subject", got, err)
			}
			if strings.Contains(fmt.Sprintf("%+v", err), "private") {
				t.Fatal("private failure leaked", err)
			}
			if strings.Contains(mode, "error") && !errors.Is(err, cause) {
				t.Fatal("error identity lost", err)
			}
		})
	}
}

func TestConcurrentResolutionCoalescesAndCancelledWaiterDoesNotPoisonLeader(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var loads atomic.Int32
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		loads.Add(1)
		close(entered)
		<-release
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	s := scope(t, registry(t, g.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	leader := make(chan error, 1)
	go func() { _, err := g.Require(s.Context()); leader <- err }()
	<-entered
	canceled, cancel := context.WithCancel(s.Context())
	cancel()
	if _, err := g.Require(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	results := make(chan error, 24)
	for range 24 {
		go func() {
			a, err := g.Require(s.Context())
			if err == nil && a.ID != 7 {
				err = errors.New("wrong subject")
			}
			results <- err
		}()
	}
	close(release)
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
	for range 24 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 {
		t.Fatal("concurrent duplicate hydration", loads.Load())
	}
}

func TestCancellationRetainsCallbackSlotAndCloseWaitsForExit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		close(entered)
		<-release
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	c := auth.DefaultConfig()
	c.MaxConcurrent = 1
	c.Timeout = 100 * time.Millisecond // Also bounds the queued admission wait.
	r, err := auth.NewRegistry(c, g.Registration())
	if err != nil {
		t.Fatal(err)
	}
	s := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	ctx, cancel := context.WithCancel(s.Context())
	finished := make(chan error, 1)
	go func() {
		a, err := g.Require(ctx)
		if a != (account{}) {
			t.Error("canceled result exposed model")
		}
		finished <- err
	}()
	<-entered
	cancel()
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	select {
	case <-finished:
		t.Fatal("abandoned callback")
	case <-closed:
		t.Fatal("close abandoned callback")
	case <-time.After(20 * time.Millisecond):
	}
	next := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if _, err := g.Require(next.Context()); !errors.Is(err, fault.Overloaded) {
		t.Fatal("released live callback slot", err)
	}
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-closed
	if _, err := g.Require(s.Context()); err == nil {
		t.Fatal("closed scope reused")
	}
}

func TestGuardNamespacesAndDeclarationRegistrationCannotCollide(t *testing.T) {
	p := provider(func(_ context.Context, key accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: key, Enabled: true}), nil
	})
	first := auth.DefineGuard("first", p, strategy(t, "first-input", 7))
	second := auth.DefineGuard("second", p, strategy(t, "second-input", 8))
	s := scope(t, registry(t, first.Registration(), second.Registration()), auth.Credential{Name: "first-input", Secret: secret.New("valid")}, auth.Credential{Name: "second-input", Secret: secret.New("valid")})
	a, err := first.Require(s.Context())
	if err != nil || a.ID != 7 {
		t.Fatal(a, err)
	}
	b, err := second.Require(s.Context())
	if err != nil || b.ID != 8 {
		t.Fatal(b, err)
	}
	alias := auth.DefineGuard("first", p, strategy(t, "first-input", 7))
	if _, err := alias.Require(s.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal("name-only guard matched registered identity", err)
	}
	if _, err := auth.NewRegistry(auth.DefaultConfig(), first.Registration(), alias.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	other := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		return value.Optional[account]{}, nil
	})
	foreign := auth.DefineGuard("foreign", other, strategy(t, "first-input", 7))
	if _, err := auth.NewRegistry(auth.DefaultConfig(), first.Registration(), foreign.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("conflicting provider accepted", err)
	}
	pol := auth.DefinePolicy("read", func(context.Context, account, document) (bool, error) {
		t.Error("unregistered policy ran")
		return true, nil
	})
	if _, err := pol.Allows(s.Context(), first, document{}); !errors.Is(err, fault.Missing) {
		t.Fatal(err)
	}
	if _, err := auth.NewRegistry(auth.DefaultConfig(), pol.Registration(), pol.Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
}

func TestPoliciesEvaluateEachResourceAndOwnFailures(t *testing.T) {
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	var calls atomic.Int32
	for _, mode := range []string{"allow", "deny", "error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("private policy failure")
			pol := auth.DefinePolicy("read", func(_ context.Context, a account, d document) (bool, error) {
				calls.Add(1)
				switch mode {
				case "deny":
					return false, nil
				case "error":
					return true, cause
				case "panic":
					panic("private policy payload")
				case "goexit":
					runtime.Goexit()
				}
				return a.ID == d.Owner, nil
			})
			s := scope(t, registry(t, g.Registration(), pol.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
			before := calls.Load()
			for range 2 {
				allowed, err := pol.Allows(s.Context(), g, document{7})
				if mode == "allow" {
					if !allowed || err != nil {
						t.Fatal(err)
					}
				} else if allowed {
					t.Fatal("failed policy allowed access")
				}
				if mode == "error" && !errors.Is(err, cause) {
					t.Fatal("cause lost")
				}
			}
			if calls.Load()-before != 2 {
				t.Fatal("policy decision cached")
			}
			if err := pol.Authorize(s.Context(), g, document{8}); err == nil {
				t.Fatal("wrong resource authorized")
			}
		})
	}
}

func TestAttributionIsMetadataAndCannotAuthenticate(t *testing.T) {
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		t.Error("metadata triggered lookup")
		return value.Optional[account]{}, nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	origin, err := (attribution.Origin{}).WithModel(account{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Require(ctx); !errors.Is(err, fault.Missing) {
		t.Fatal("attribution authenticated", err)
	}
	r := registry(t, g.Registration())
	s, err := r.NewScope(ctx, auth.Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := g.Require(s.Context()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("attribution became credential", err)
	}
	if attribution.FromContext(s.Context()) != origin {
		t.Fatal("lost request metadata")
	}
}

func TestRecursiveResolutionFailsWithoutDeadlocking(t *testing.T) {
	var g auth.Guard[account]
	p := provider(func(ctx context.Context, _ accountKey) (value.Optional[account], error) {
		_, err := g.Require(ctx)
		return value.Optional[account]{}, err
	})
	g = auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	s := scope(t, registry(t, g.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if _, err := g.Require(s.Context()); !errors.Is(err, fault.Cycle) {
		t.Fatal("recursive lookup was not detected", err)
	}
}

func TestIdentityCodecRunsOnceAndNeverUsesPresentation(t *testing.T) {
	var decodes atomic.Int32
	stored := codec.New(func(k accountKey) (driver.Value, error) { return int64(k), nil }, func(v any) (accountKey, error) { decodes.Add(1); return accountKey(v.(int64)), nil })
	ref := model.NewReference[account]("accounts", accountKey(0), stored)
	p := auth.DefineProvider("accounts", ref, func(context.Context, accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: 7, Enabled: true}), nil
	}, func(context.Context, account) (bool, error) { return true, nil })
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	s := scope(t, registry(t, g.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if _, err := g.Require(s.Context()); err != nil {
		t.Fatal(err)
	}
	if decodes.Load() != 1 {
		t.Fatal("repeated stored identity decoding", decodes.Load())
	}
}

func TestCredentialBoundsOwnershipAndSafeFormatting(t *testing.T) {
	items := []auth.Credential{{Name: "input", Secret: secret.New("private-value")}}
	c := inputs(t, items...)
	items[0].Secret = secret.New("changed")
	if c.Get("input").Reveal() != "private-value" {
		t.Fatal("input mutation changed snapshot")
	}
	for _, v := range []any{c, proof(t, 7, auth.Authenticated)} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), "private-value") {
				t.Fatal("formatted secret")
			}
		}
		encoded, err := json.Marshal(v)
		if err != nil || strings.Contains(string(encoded), "private-value") {
			t.Fatal("serialized secret", err)
		}
	}
	cases := [][]auth.Credential{
		{{Name: "bad source", Secret: secret.New("a")}},
		{{Name: "input", Secret: secret.New("a")}, {Name: "input", Secret: secret.New("b")}},
		{{Name: "input", Secret: secret.New("")}},
		{{Name: "input", Secret: secret.New(strings.Repeat("x", auth.MaxCredentialBytes+1))}},
		make([]auth.Credential, auth.MaxCredentials+1),
	}
	for _, bad := range cases {
		if _, err := auth.NewCredentials(bad...); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
	var maximal []auth.Credential
	for n := 0; n < auth.MaxCredentialsBytes/auth.MaxCredentialBytes; n++ {
		maximal = append(maximal, auth.Credential{Name: auth.CredentialName(fmt.Sprintf("input-%d", n)), Secret: secret.New(strings.Repeat("x", auth.MaxCredentialBytes))})
	}
	if _, err := auth.NewCredentials(maximal...); err != nil {
		t.Fatal("exact maximum failed", err)
	}
	maximal = append(maximal, auth.Credential{Name: "extra", Secret: secret.New("x")})
	if _, err := auth.NewCredentials(maximal...); err == nil {
		t.Fatal("aggregate credential bound ignored")
	}
}

func TestRegistryAndScopeRejectInvalidConstruction(t *testing.T) {
	for _, c := range []auth.Config{{}, {MaxConcurrent: 1}, {MaxConcurrent: 65537, Timeout: time.Second}, {MaxConcurrent: 1, Timeout: -1}} {
		if _, err := auth.NewRegistry(c); err == nil {
			t.Fatal("invalid limits")
		}
	}
	for _, items := range [][]auth.Registration{{{}}, make([]auth.Registration, auth.MaxRegistrations+1)} {
		if _, err := auth.NewRegistry(auth.DefaultConfig(), items...); err == nil {
			t.Fatal("invalid registrations")
		}
	}
	var g auth.Guard[account]
	var pol auth.Policy[account, document]
	var p auth.Provider[account, accountKey]
	var st auth.Strategy[account, accountKey]
	for _, err := range []error{g.Validate(), pol.Validate(), p.Validate(), st.Validate()} {
		if err == nil {
			t.Fatal("zero declaration accepted")
		}
	}
	if _, err := auth.NewProof(account{ID: 7}.FoundryReference(), auth.Assurance(0)); err == nil {
		t.Fatal("zero assurance accepted")
	}
	r := registry(t)
	if _, err := r.NewScope(nil, auth.Credentials{}); err == nil {
		t.Fatal("nil scope context")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.NewScope(canceled, auth.Credentials{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s := scope(t, r)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestTimeoutSuppressesLateSuccessAndCannotRetryWithinScope(t *testing.T) {
	var calls atomic.Int32
	p := provider(func(ctx context.Context, _ accountKey) (value.Optional[account], error) {
		calls.Add(1)
		<-ctx.Done()
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	guard := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	config := auth.DefaultConfig()
	config.Timeout = 10 * time.Millisecond
	registry, err := auth.NewRegistry(config, guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	scoped := scope(t, registry, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	for range 2 {
		got, err := guard.Require(scoped.Context())
		if got != (account{}) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("late subject escaped timeout", got, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("timed-out provider silently retried")
	}
}

func TestStoredModelNamespaceIsCheckedEvenWithMatchingGoTypes(t *testing.T) {
	var calls atomic.Int32
	provider := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		calls.Add(1)
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	foreign, err := auth.NewProof(model.NewReference[account]("different_accounts", accountKey(7), codec.Signed[accountKey]()), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	verifier := auth.DefineStrategy("bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		return value.Set(foreign), nil
	})
	guard := auth.DefineGuard("api", provider, verifier)
	scoped := scope(t, registry(t, guard.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if _, err := guard.Require(scoped.Context()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("foreign model namespace accepted", err)
	}
	if calls.Load() != 0 {
		t.Fatal("foreign identity reached provider")
	}
}
