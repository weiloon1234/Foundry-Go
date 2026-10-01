package auth_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestBoundCredentialsVerifyOnlyThroughTheirOwnStrategy(t *testing.T) {
	provider := provider(func(_ context.Context, id accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: id, Enabled: true}), nil
	})
	verified := 0
	strategy, binder := auth.BindStrategy(auth.DefineStrategy("api.bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		return value.Optional[auth.Proof[account, accountKey]]{}, nil
	}), func(_ context.Context, id accountKey) (value.Optional[auth.Proof[account, accountKey]], error) {
		verified++
		proof, err := auth.NewProof(account{ID: id}.FoundryReference(), auth.Authenticated)
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("api", provider, strategy)
	_, foreignBinder := auth.BindStrategy(auth.DefineStrategy("api.bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		return value.Optional[auth.Proof[account, accountKey]]{}, nil
	}), func(context.Context, accountKey) (value.Optional[auth.Proof[account, accountKey]], error) {
		t.Fatal("foreign verifier ran")
		return value.Optional[auth.Proof[account, accountKey]]{}, nil
	})
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(credentials auth.Credentials) (account, error) {
		scope, err := registry.NewScope(t.Context(), credentials)
		if err != nil {
			t.Fatal(err)
		}
		defer scope.Close()
		return guard.Require(scope.Context())
	}
	bound, err := binder.Bind(1)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.Credentials{}.WithBound("api.bearer", bound)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := resolve(credentials); err != nil || user.ID != 1 || verified != 1 {
		t.Fatal("bound credential did not authenticate its guard", user, err)
	}
	// Every scope re-verifies the bound credential.
	if _, err := resolve(credentials); err != nil || verified != 2 {
		t.Fatal("bound credential was not re-verified", err)
	}
	foreign, _ := foreignBinder.Bind(1)
	other, _ := auth.Credentials{}.WithBound("api.bearer", foreign)
	if _, err := resolve(other); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("another strategy's bound credential authenticated", err)
	}
	present, err := auth.NewCredentials(auth.Credential{Name: "api.bearer", Secret: secret.New("transport")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := present.WithBound("api.bearer", bound); !errors.Is(err, fault.Duplicate) {
		t.Fatal("ambiguous secret and bound credential accepted", err)
	}
	if _, err := (auth.Credentials{}).WithBound("api.bearer", auth.BoundCredential{}); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty bound credential accepted")
	}
	if text := fmt.Sprintf("%v %+v", bound, credentials); text != "bound authentication credential authentication credentials" {
		t.Fatal("bound credential formatting leaked contents", text)
	}
	unbound := auth.DefineGuard("plain", provider, auth.DefineStrategy("plain.bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		return value.Optional[auth.Proof[account, accountKey]]{}, nil
	}))
	plainRegistry, err := auth.NewRegistry(auth.DefaultConfig(), unbound.Registration())
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := auth.Credentials{}.WithBound("plain.bearer", bound)
	scope, err := plainRegistry.NewScope(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	if _, err := unbound.Require(scope.Context()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("strategy without a binder accepted a bound credential", err)
	}
	broken, _ := auth.BindStrategy[account, accountKey, accountKey](auth.DefineStrategy("x.bearer", func(context.Context, secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		return value.Optional[auth.Proof[account, accountKey]]{}, nil
	}), nil)
	if err := broken.Validate(); !errors.Is(err, fault.Invalid) {
		t.Fatal("bindable strategy without a verifier validated")
	}
}
