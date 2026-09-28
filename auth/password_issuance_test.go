package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPasswordProofRechecksStoredHashIdentityEligibilityAndMFA(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "private password")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	original := passwordSubject{ID: 7, Digest: hash, Enabled: true}
	for _, change := range []string{"current", "hash", "identity", "disabled", "mfa", "missing", "error"} {
		t.Run(change, func(t *testing.T) {
			current := original
			present := true
			calls := 0
			var lookupError error
			binding := loginBinding(original)
			binding.Lock = func(context.Context, *database.Tx, passwordSubject) (value.Optional[passwordSubject], error) {
				calls++
				if lookupError != nil {
					return value.Optional[passwordSubject]{}, lookupError
				}
				if !present {
					return value.Optional[passwordSubject]{}, nil
				}
				return value.Set(current), nil
			}
			login := loginInstance(t, h, binding)
			result, err := login.Authenticate(t.Context(), "member@example.test", plain)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 0 || !result.Proof().HasIssuanceCheck() {
				t.Fatal("issuance check ran during verification or was absent")
			}
			cause := errors.New("locked lookup failed")
			switch change {
			case "hash":
				current.Digest = replacement
			case "identity":
				current.ID++
			case "disabled":
				current.Enabled = false
			case "mfa":
				current.MFA = true
			case "missing":
				present = false
			case "error":
				lookupError = cause
			}
			err = result.Proof().CheckIssuance(t.Context(), &database.Tx{})
			if change == "current" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("changed model accepted")
			}
			if change == "error" && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if calls != 1 {
				t.Fatal("lock callback repeated")
			}
		})
	}
}
func TestScopeNarrowingRetainsPasswordIssuanceCheck(t *testing.T) {
	reference := (passwordSubject{ID: 7}).FoundryReference()
	proof, err := auth.NewProof(reference, auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	proof, err = proof.WithIssuanceCheck(func(context.Context, *database.Tx) error { calls++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	read := auth.DefineAccessScope[passwordSubject]("members.read")
	grants, err := auth.NewAccessScopes(read)
	if err != nil {
		t.Fatal(err)
	}
	narrowed, err := proof.WithAccessScopes(grants)
	if err != nil {
		t.Fatal(err)
	}
	if !narrowed.HasIssuanceCheck() {
		t.Fatal("scope narrowing discarded check")
	}
	if err := narrowed.CheckIssuance(t.Context(), &database.Tx{}); err != nil || calls != 1 {
		t.Fatal("check not retained", err)
	}
	wider, err := auth.NewAccessScopes(read, auth.DefineAccessScope[passwordSubject]("members.write"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := narrowed.WithAccessScopes(wider); !errors.Is(err, auth.Forbidden) {
		t.Fatal("scope grant expanded", err)
	}
	if _, err := narrowed.WithIssuanceCheck(func(context.Context, *database.Tx) error { return nil }); err == nil {
		t.Fatal("check replaced")
	}
}
