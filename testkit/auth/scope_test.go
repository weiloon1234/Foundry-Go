package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	authtest "github.com/weiloon1234/Foundry-Go/testkit/auth"
	"github.com/weiloon1234/Foundry-Go/value"
)

type member struct {
	ID      int64
	Enabled bool
}

func (m member) reference() model.Reference[member, int64] {
	return model.NewReference[member]("auth_test_members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

func TestScopeHelpersKeepVerificationEligibilityAndAuthorization(t *testing.T) {
	state := member{ID: 7, Enabled: true}
	loads, verifies := 0, 0
	provider := auth.DefineProvider("members", state.reference(), func(context.Context, int64) (value.Optional[member], error) { loads++; return value.Set(state), nil }, func(_ context.Context, m member) (bool, error) { return m.Enabled, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, input secret.String) (value.Optional[auth.Proof[member, int64]], error) {
		verifies++
		if input.Reveal() == "invalid" {
			return value.Optional[auth.Proof[member, int64]]{}, nil
		}
		assurance := auth.Authenticated
		if input.Reveal() == "pending" {
			assurance = auth.PendingMFA
		}
		proof, err := auth.NewProof(state.reference(), assurance)
		return value.Set(proof), err
	})
	guard := auth.DefineGuard("api", provider, strategy)
	policy := auth.DefinePolicy("owned.read", func(_ context.Context, m member, owner int64) (bool, error) { return m.ID == owner, nil })
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), policy.Registration())
	if err != nil {
		t.Fatal(err)
	}
	var retained context.Context
	t.Run("owned", func(t *testing.T) {
		scope := authtest.Scope(t, registry, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
		retained = scope.Context()
		got := authtest.Require(t, scope, guard)
		if got.ID != 7 {
			t.Fatal("wrong model")
		}
		if err := policy.Authorize(scope.Context(), guard, 7); err != nil {
			t.Fatal(err)
		}
		if err := policy.Authorize(scope.Context(), guard, 8); !errors.Is(err, auth.Forbidden) {
			t.Fatal("helper bypassed policy", err)
		}
		_ = authtest.Require(t, scope, guard)
		if loads != 1 || verifies != 1 {
			t.Fatal("helper bypassed/coalesced incorrectly", loads, verifies)
		}
	})
	if _, err := guard.Require(context.WithoutCancel(retained)); err == nil {
		t.Fatal("test cleanup left scope usable")
	}
	for _, mode := range []string{"anonymous", "invalid", "pending", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			before := loads
			inputs := []auth.Credential{{Name: "bearer", Secret: secret.New(mode)}}
			if mode == "anonymous" {
				inputs = nil
			}
			if mode == "disabled" {
				state.Enabled = false
				defer func() { state.Enabled = true }()
			}
			scope := authtest.Scope(t, registry, inputs...)
			origin, err := (attribution.Origin{}).WithModel(state)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := attribution.WithContext(scope.Context(), origin)
			if err != nil {
				t.Fatal(err)
			}
			_, err = guard.Require(ctx)
			if mode == "pending" {
				if !errors.Is(err, auth.MFARequired) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, auth.Unauthenticated) {
				t.Fatal("helper accepted failed authentication", err)
			}
			if mode != "disabled" && loads != before {
				t.Fatal("failed credential loaded a model")
			}
		})
	}
}
