package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type credentialInfo struct{ impersonated bool }

// A scope authenticated by an impersonation credential can read its metadata
// but never obtain a proof to mint another credential for the subject.
func TestCurrentProofRefusesImpersonatedCredentials(t *testing.T) {
	p := provider(func(_ context.Context, id accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: id, Enabled: true}), nil
	})
	slot := auth.NewCredentialSlot[credentialInfo]()
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[account, accountKey]], error) {
		proof, err := auth.NewProof(account{ID: 7}.FoundryReference(), auth.Authenticated)
		if err != nil {
			return value.Optional[auth.Proof[account, accountKey]]{}, err
		}
		if credential.Reveal() == "impersonated" {
			proof, err = auth.AttachImpersonatedCredential(proof, slot, credentialInfo{impersonated: true})
		} else {
			proof, err = auth.AttachCredential(proof, slot, credentialInfo{})
		}
		return value.Set(proof), err
	})
	g := auth.DefineGuard("api", p, strategy)
	r := registry(t, g.Registration())
	ordinary := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if _, err := auth.CurrentProof(ordinary.Context(), p, g); err != nil {
		t.Fatal(err)
	}
	impersonated := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("impersonated")})
	if info, err := auth.CurrentCredential(impersonated.Context(), g, slot); err != nil || !info.impersonated {
		t.Fatal("impersonation metadata is not readable", err)
	}
	if _, err := auth.CurrentProof(impersonated.Context(), p, g); !errors.Is(err, auth.ImpersonationForbidden) {
		t.Fatal("impersonation credential produced a proof", err)
	}
}

// Nested policy evaluation inside another callback of the same scope reuses the
// parent's slot instead of queueing behind itself on a saturated registry.
func TestNestedEvaluationReusesTheParentSlot(t *testing.T) {
	p := provider(func(_ context.Context, id accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: id, Enabled: true}), nil
	})
	g := auth.DefineGuard("api", p, strategy(t, "bearer", 7))
	inner := auth.DefinePolicy("documents.inner", func(_ context.Context, a account, d document) (bool, error) { return a.ID == d.Owner, nil })
	outer := auth.DefinePolicy("documents.outer", func(ctx context.Context, _ account, d document) (bool, error) {
		return inner.Allows(ctx, g, d)
	})
	config := auth.DefaultConfig()
	config.MaxConcurrent = 1
	config.Timeout = 2 * time.Second
	r, err := auth.NewRegistry(config, g.Registration(), inner.Registration(), outer.Registration())
	if err != nil {
		t.Fatal(err)
	}
	s := scope(t, r, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	started := time.Now()
	allowed, err := outer.Allows(s.Context(), g, document{Owner: 7})
	if err != nil || !allowed {
		t.Fatal("nested evaluation failed on a saturated registry", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatal("nested evaluation waited for its own parent's slot", elapsed)
	}
}
