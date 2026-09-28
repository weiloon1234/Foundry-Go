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

func TestPermissionsUseCurrentDecisionsAndShareGuardResolution(t *testing.T) {
	var loads atomic.Int32
	p := provider(func(context.Context, accountKey) (value.Optional[account], error) {
		loads.Add(1)
		return value.Set(account{ID: 7, Enabled: true}), nil
	})
	guard := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	allowed := true
	var failure error
	calls := 0
	permission := auth.DefinePermission("accounts.manage", func(_ context.Context, subject account) (bool, error) {
		calls++
		if subject.ID != 7 {
			t.Error("permission received a different model")
		}
		return allowed, failure
	})
	r := registry(t, guard.Registration(), permission.Registration())
	owned := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if err := permission.Authorize(owned.Context(), guard); err != nil {
		t.Fatal(err)
	}
	allowed = false
	if ok, err := permission.Allows(owned.Context(), guard); err != nil || ok {
		t.Fatal("decision cached", err)
	}
	if err := permission.Authorize(owned.Context(), guard); !errors.Is(err, auth.Forbidden) {
		t.Fatal("permission revocation ignored", err)
	}
	failure = errors.New("role authority unavailable")
	allowed = true
	if ok, err := permission.Allows(owned.Context(), guard); ok || !errors.Is(err, failure) {
		t.Fatal("authority failure allowed access", err)
	}
	if loads.Load() != 1 || calls != 4 {
		t.Fatal("permission rehydrated or skipped evaluation", loads.Load(), calls)
	}
	anonymous := scope(t, r)
	if err := permission.Authorize(anonymous.Context(), guard); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("permission accepted anonymous model", err)
	}
	if calls != 4 {
		t.Fatal("anonymous reached decision")
	}
}
func TestPermissionsRequireExactExplicitRegistration(t *testing.T) {
	check := func(context.Context, account) (bool, error) { return true, nil }
	first := auth.DefinePermission("accounts.manage", check)
	other := auth.DefinePermission("accounts.manage", check)
	r := registry(t, first.Registration())
	if err := first.ValidateIn(r); err != nil {
		t.Fatal(err)
	}
	if err := other.ValidateIn(r); !errors.Is(err, fault.Missing) {
		t.Fatal("same name replaced declaration", err)
	}
	policy := auth.DefinePolicy("accounts.manage", func(context.Context, account, document) (bool, error) { return true, nil })
	for _, duplicate := range []auth.Registration{first.Registration(), other.Registration(), policy.Registration()} {
		if _, err := auth.NewRegistry(auth.DefaultConfig(), first.Registration(), duplicate); !errors.Is(err, fault.Duplicate) {
			t.Fatal("duplicate policy namespace accepted", err)
		}
	}
	for _, permission := range []auth.Permission[account]{{}, auth.DefinePermission[account]("empty.callback", nil), auth.DefinePermission("*", check)} {
		if err := permission.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid permission accepted", err)
		}
	}
	if err := first.ValidateIn(nil); err == nil {
		t.Fatal("nil registry accepted")
	}
}
