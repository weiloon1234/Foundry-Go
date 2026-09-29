package auth_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type operator struct{ ID int64 }

// Before hooks decide first (a verified super-administrator, a suspended
// account); After hooks can only veto an allow. Hooks for another model type
// never run, and a failing hook denies instead of granting.
func TestPolicyHooksRunAroundPoliciesAndPermissions(t *testing.T) {
	p := provider(func(_ context.Context, id accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: id, Enabled: true, Permission: id == 1}), nil
	})
	var evaluated atomic.Int32
	read := auth.DefinePolicy("documents.read", func(_ context.Context, a account, d document) (bool, error) {
		evaluated.Add(1)
		return a.ID == d.Owner, nil
	})
	export := auth.DefinePermission("documents.export", func(context.Context, account) (bool, error) {
		evaluated.Add(1)
		return true, nil
	})
	failure := errors.New("private hook failure")
	before := auth.DefineBefore("documents.admin", func(_ context.Context, a account, policy auth.PolicyName) (auth.Verdict, error) {
		switch a.ID {
		case 1:
			return auth.Allow, nil
		case 2:
			return auth.Deny, nil
		case 3:
			return auth.Abstain, failure
		}
		return auth.Abstain, nil
	})
	readOnly := auth.DefineAfter("documents.read_only", func(_ context.Context, a account, policy auth.PolicyName) (auth.Verdict, error) {
		if a.ID == 4 && policy == "documents.export" {
			return auth.Deny, nil
		}
		return auth.Allow, nil // Allow never overrides a denial after the policy.
	})
	foreign := auth.DefineBefore("operators.block", func(context.Context, operator, auth.PolicyName) (auth.Verdict, error) {
		t.Error("hook for another model ran")
		return auth.Deny, nil
	})
	for _, c := range []struct {
		key                   accountKey
		read, export, invoked bool
		failed                bool
	}{
		{key: 1, read: true, export: true},
		{key: 2},
		{key: 3, failed: true},
		{key: 4, read: false, export: false, invoked: true},
		{key: 5, read: false, export: true, invoked: true},
	} {
		g := auth.DefineGuard("api", p, strategy(t, "bearer", c.key))
		r := registry(t, g.Registration(), read.Registration(), export.Registration(), before.Registration(), readOnly.Registration(), foreign.Registration())
		s := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
		evaluated.Store(0)
		allowed, err := read.Allows(s.Context(), g, document{Owner: 99})
		if c.failed {
			if allowed || !errors.Is(err, failure) {
				t.Fatal("failing hook granted access", c.key, err)
			}
			continue
		}
		if err != nil || allowed != c.read {
			t.Fatal("policy decision", c.key, allowed, err)
		}
		exported, err := export.Allows(s.Context(), g)
		if err != nil || exported != c.export {
			t.Fatal("permission decision", c.key, exported, err)
		}
		if (evaluated.Load() > 0) != c.invoked {
			t.Fatal("policy callback invocation", c.key, evaluated.Load())
		}
	}
}

// A policy can explain a denial with a typed code and safe message; Inspect
// exposes it, and Authorize returns it while remaining auth.Forbidden.
func TestTypedDenialAndInspect(t *testing.T) {
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	edit := auth.DefinePolicy("documents.edit", func(_ context.Context, _ account, d document) (bool, error) {
		if d.Owner == 0 {
			return false, auth.NewDenial("documents.archived", "Archived documents cannot be edited.")
		}
		return true, nil
	})
	s := scope(t, registry(t, g.Registration(), edit.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	decision, err := edit.Inspect(s.Context(), g, document{})
	if err != nil || decision.Allowed() {
		t.Fatal("denial became an error or allow", err)
	}
	denial, present := decision.Denial()
	if !present || denial.Code() != "documents.archived" || denial.Message() != "Archived documents cannot be edited." {
		t.Fatal("typed denial lost")
	}
	err = edit.Authorize(s.Context(), g, document{})
	var typed *auth.Denial
	if !errors.Is(err, auth.Forbidden) || !errors.As(err, &typed) || typed.Code() != "documents.archived" {
		t.Fatal("authorize lost the typed denial", err)
	}
	if allowed, err := edit.Allows(s.Context(), g, document{}); allowed || err != nil {
		t.Fatal("denial is not an operational error", err)
	}
	if decision, err := edit.Inspect(s.Context(), g, document{Owner: 7}); err != nil || !decision.Allowed() || decision.Err() != nil {
		t.Fatal(err)
	}
}

// Guest policies evaluate anonymous requests with an omitted model; invalid or
// pending credentials still fail rather than falling back to guest.
func TestGuestPolicyAcceptsAnonymousButNotInvalidCredentials(t *testing.T) {
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	view := auth.DefineGuestPolicy("documents.view", func(_ context.Context, a value.Optional[account], d document) (bool, error) {
		if current, present := a.Get(); present {
			return current.ID == d.Owner, nil
		}
		return d.Owner == 0, nil // Public documents only.
	})
	r := registry(t, g.Registration(), view.Registration())
	anonymous := scope(t, r)
	if allowed, err := view.Allows(anonymous.Context(), g, document{}); err != nil || !allowed {
		t.Fatal("public document denied to a guest", err)
	}
	if err := view.Authorize(anonymous.Context(), g, document{Owner: 7}); !errors.Is(err, auth.Forbidden) {
		t.Fatal("private document allowed to a guest", err)
	}
	member := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if allowed, err := view.Allows(member.Context(), g, document{Owner: 7}); err != nil || !allowed {
		t.Fatal("owner denied", err)
	}
	invalid := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("forged")})
	if allowed, err := view.Allows(invalid.Context(), g, document{}); allowed || !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("invalid credential fell back to guest", err)
	}
}

// Derived registries add guards to a shared application registry without
// modifying it, keep its policies and callback capacity, and reject duplicates.
func TestRegistryWithExtendsSharedDeclarations(t *testing.T) {
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	read := auth.DefinePolicy("documents.read", func(_ context.Context, a account, d document) (bool, error) { return a.ID == d.Owner, nil })
	shared := registry(t, read.Registration())
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	derived, err := shared.With(g.Registration())
	if err != nil {
		t.Fatal(err)
	}
	s := scope(t, derived, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if err := read.Authorize(s.Context(), g, document{Owner: 7}); err != nil {
		t.Fatal("derived registry lost shared policy", err)
	}
	if _, err := shared.With(auth.DefinePolicy("documents.read", func(context.Context, account, document) (bool, error) { return true, nil }).Registration()); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate policy name accepted", err)
	}
	unrelated := scope(t, shared, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if _, err := g.Require(unrelated.Context()); err == nil {
		t.Fatal("derivation modified the shared registry")
	}
}

// After vetoes also apply when a Before hook allowed: a global read-only or
// suspension switch stops super-administrators too, and still never grants.
func TestAfterVetoAppliesAfterBeforeAllow(t *testing.T) {
	p := provider(func(_ context.Context, id accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: id, Enabled: true}), nil
	})
	var evaluated atomic.Int32
	read := auth.DefinePolicy("documents.read", func(context.Context, account, document) (bool, error) {
		evaluated.Add(1)
		return false, nil
	})
	superAdmin := auth.DefineBefore("documents.super_admin", func(context.Context, account, auth.PolicyName) (auth.Verdict, error) {
		return auth.Allow, nil
	})
	var readOnly atomic.Bool
	killSwitch := auth.DefineAfter("documents.read_only", func(context.Context, account, auth.PolicyName) (auth.Verdict, error) {
		if readOnly.Load() {
			return auth.Deny, nil
		}
		return auth.Allow, nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 1))
	s := scope(t, registry(t, g.Registration(), read.Registration(), superAdmin.Registration(), killSwitch.Registration()), auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if allowed, err := read.Allows(s.Context(), g, document{}); err != nil || !allowed {
		t.Fatal("Before allow was lost", err)
	}
	readOnly.Store(true)
	if allowed, err := read.Allows(s.Context(), g, document{}); err != nil || allowed {
		t.Fatal("After veto did not stop a Before allow", err)
	}
	if evaluated.Load() != 0 {
		t.Fatal("policy callback ran after a Before decision")
	}
}
