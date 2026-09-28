package auth_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func accountScopes(t *testing.T, names ...auth.AccessScopeName) auth.AccessScopes[account] {
	t.Helper()
	declarations := make([]auth.AccessScope[account], len(names))
	for i, name := range names {
		declarations[i] = auth.DefineAccessScope[account](name)
	}
	result, err := auth.NewAccessScopes(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAccessScopesAreBoundedImmutableModelOwnedSets(t *testing.T) {
	read, write := auth.DefineAccessScope[account]("orders.read"), auth.DefineAccessScope[account]("orders.write")
	declarations := []auth.AccessScope[account]{write, read}
	granted, err := auth.NewAccessScopes(declarations...)
	if err != nil {
		t.Fatal(err)
	}
	declarations[0] = auth.DefineAccessScope[account]("changed")
	names := granted.Names()
	if !slices.Equal(names, []auth.AccessScopeName{"orders.read", "orders.write"}) {
		t.Fatal(names)
	}
	names[0] = "changed"
	if !granted.Contains(read) || !granted.Contains(write) || granted.Len() != 2 {
		t.Fatal("mutable grant")
	}
	if !granted.ContainsAll(accountScopes(t, "orders.read")) || granted.ContainsAll(accountScopes(t, "other")) {
		t.Fatal("incorrect inclusion")
	}
	empty := auth.AccessScopes[account]{}
	if empty.Len() != 0 || empty.Contains(read) || !granted.ContainsAll(empty) {
		t.Fatal("empty grant")
	}
	for _, name := range []auth.AccessScopeName{"", "*", "orders.*", "orders:read", " orders.read", auth.AccessScopeName(strings.Repeat("a", 129))} {
		if _, err := auth.NewAccessScopes(auth.DefineAccessScope[account](name)); !errors.Is(err, fault.Invalid) {
			t.Fatalf("invalid scope accepted: %q: %v", name, err)
		}
	}
	if _, err := auth.NewAccessScopes(read, read); !errors.Is(err, fault.Duplicate) {
		t.Fatal(err)
	}
	many := make([]auth.AccessScope[account], auth.MaxAccessScopes+1)
	for i := range many {
		many[i] = auth.DefineAccessScope[account](auth.AccessScopeName(fmt.Sprintf("scope.%d", i)))
	}
	if _, err := auth.NewAccessScopes(many...); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := auth.NewAccessScopes(many[:auth.MaxAccessScopes]...); err != nil {
		t.Fatal(err)
	}
	p, err := auth.NewScopedProof(account{ID: 7}.FoundryReference(), auth.Authenticated, granted)
	if err != nil {
		t.Fatal(err)
	}
	restored, scoped := p.AccessScopes()
	restored.Names()[0] = "changed"
	if !scoped || !restored.Contains(read) {
		t.Fatal("proof lost immutable grant")
	}
	p, err = auth.NewScopedProof(account{ID: 7}.FoundryReference(), auth.Authenticated, empty)
	restored, scoped = p.AccessScopes()
	if err != nil || !scoped || restored.Len() != 0 {
		t.Fatal("empty grants became unscoped", err)
	}
	if _, scoped := proof(t, 7, auth.Authenticated).AccessScopes(); scoped {
		t.Fatal("unscoped proof claims a grant")
	}
	if _, err := auth.NewScopedProof(account{ID: 7}.FoundryReference(), auth.Assurance(0), granted); err == nil {
		t.Fatal("invalid assurance")
	}
}

func TestScopedGuardSharesModelAndRefreshesPolicyAtNextRequest(t *testing.T) {
	var verifies, loads atomic.Int32
	var allowed atomic.Bool
	allowed.Store(true)
	granted := accountScopes(t, "orders.read", "profile.read")
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		loads.Add(1)
		return value.Set(account{ID: 7, Enabled: true, Permission: allowed.Load()}), nil
	})
	verified, err := auth.NewScopedProof(account{ID: 7}.FoundryReference(), auth.Authenticated, granted)
	if err != nil {
		t.Fatal(err)
	}
	st := auth.DefineStrategy("bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		verifies.Add(1)
		return value.Set(verified), nil
	})
	guard := auth.DefineGuard("api", p, st)
	policy := auth.DefinePolicy("order.read", func(_ context.Context, a account, d document) (bool, error) {
		return a.Permission && a.ID == d.Owner, nil
	})
	registry := registry(t, guard.Registration(), policy.Registration())
	input := auth.Credential{Name: "bearer", Secret: secret.New("verified")}
	current := scope(t, registry, input)
	var workers sync.WaitGroup
	for range 24 {
		workers.Go(func() {
			if a, err := guard.RequireScopes(current.Context(), granted); err != nil || a.ID != 7 {
				t.Error("scope resolution", err)
			}
			if _, err := guard.Require(current.Context()); err != nil {
				t.Error(err)
			}
			if _, err := guard.Optional(current.Context()); err != nil {
				t.Error(err)
			}
			if err := policy.Authorize(current.Context(), guard, document{7}); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if verifies.Load() != 1 || loads.Load() != 1 {
		t.Fatal("scope checks rehydrated model", verifies.Load(), loads.Load())
	}
	if a, err := guard.RequireScopes(current.Context(), accountScopes(t, "orders.write")); !errors.Is(err, auth.Forbidden) || a != (account{}) {
		t.Fatal("missing scope", a, err)
	}
	allowed.Store(false)
	next := scope(t, registry, input)
	if _, err := guard.RequireScopes(next.Context(), granted); err != nil {
		t.Fatal(err)
	}
	if err := policy.Authorize(next.Context(), guard, document{7}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("scope bypassed current model policy", err)
	}
	if err := current.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.RequireScopes(context.WithoutCancel(current.Context()), granted); err == nil {
		t.Fatal("closed scope reused")
	}
}

func TestScopedGuardRejectsInvalidMissingAndPendingAuthority(t *testing.T) {
	required := accountScopes(t, "orders.read")
	for _, test := range []struct {
		name string
		want error
	}{
		{"absent", auth.Unauthenticated}, {"invalid", auth.Unauthenticated}, {"disabled", auth.Unauthenticated},
		{"pending", auth.MFARequired}, {"unscoped", auth.Forbidden}, {"empty", auth.Forbidden}, {"different", auth.Forbidden}, {"valid", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var loads atomic.Int32
			p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
				loads.Add(1)
				return value.Set(account{ID: 7, Enabled: test.name != "disabled"}), nil
			})
			grant := required
			if test.name == "empty" {
				grant = auth.AccessScopes[account]{}
			}
			if test.name == "different" {
				grant = accountScopes(t, "profile.read")
			}
			assurance := auth.Authenticated
			if test.name == "pending" {
				assurance = auth.PendingMFA
			}
			verified, err := auth.NewScopedProof(account{ID: 7}.FoundryReference(), assurance, grant)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "unscoped" {
				verified = proof(t, 7, auth.Authenticated)
			}
			strategy := auth.DefineStrategy("bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
				if test.name == "invalid" {
					return value.Optional[auth.Proof[account, accountKey]]{}, auth.Unauthenticated
				}
				return value.Set(verified), nil
			})
			guard := auth.DefineGuard("api", p, strategy)
			credentials := []auth.Credential{{Name: "bearer", Secret: secret.New("verified")}}
			if test.name == "absent" {
				credentials = nil
			}
			s := scope(t, registry(t, guard.Registration()), credentials...)
			a, err := guard.RequireScopes(s.Context(), required)
			if !errors.Is(err, test.want) || (err != nil && a != (account{})) {
				t.Fatal(a, err)
			}
			if (test.name == "absent" || test.name == "invalid" || test.name == "pending") && loads.Load() != 0 {
				t.Fatal("invalid credential hydrated")
			}
			if _, err := guard.RequireScopes(s.Context(), auth.AccessScopes[account]{}); !errors.Is(err, fault.Invalid) {
				t.Fatal("empty requirement accepted", err)
			}
		})
	}
}
