package auth_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPasswordRecheckRetainsProviderAndCurrentLockedModel(t *testing.T) {
	hasher := loginHasher(t, 2)
	plain := loginPlain(t, "private password")
	hash, err := hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	original := passwordSubject{ID: 7, Digest: hash, Enabled: true, MFA: true}
	current := original
	present := true
	locks := 0
	provider := passwordProvider(func(_ context.Context, subject passwordSubject) (bool, error) { return subject.Enabled, nil })
	binding := loginBinding(original)
	binding.Lock = func(_ context.Context, tx *database.Tx, prior passwordSubject) (value.Optional[passwordSubject], error) {
		locks++
		if tx == nil || prior != original {
			t.Fatal("lost transaction or original model")
		}
		if !present {
			return value.Optional[passwordSubject]{}, nil
		}
		return value.Set(current), nil
	}
	login, err := auth.NewPasswordLogin(provider, hasher, binding, auth.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := login.Authenticate(t.Context(), "member@example.test", plain)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proof().Assurance() != auth.PendingMFA || locks != 0 {
		t.Fatal("wrong password stage")
	}
	got, err := provider.RecheckPassword(t.Context(), &database.Tx{}, result)
	if err != nil || got != current || locks != 1 {
		t.Fatal("model recheck", err)
	}
	// Same name/type is not the same provider declaration. Rejection must happen
	// before calling a lock belonging to the other provider's domain.
	other := passwordProvider(func(context.Context, passwordSubject) (bool, error) { return true, nil })
	got, err = other.RecheckPassword(t.Context(), &database.Tx{}, result)
	if err == nil || got != (passwordSubject{}) || locks != 1 {
		t.Fatal("different provider accepted")
	}
	for _, change := range []string{"hash", "disabled", "policy", "identity", "missing"} {
		t.Run(change, func(t *testing.T) {
			current, present = original, true
			switch change {
			case "hash":
				current.Digest = changed
			case "disabled":
				current.Enabled = false
			case "policy":
				current.MFA = false
			case "identity":
				current.ID++
			case "missing":
				present = false
			}
			got, err := provider.RecheckPassword(t.Context(), &database.Tx{}, result)
			if err == nil || got != (passwordSubject{}) {
				t.Fatal("stale recheck returned model")
			}
			if err := result.Proof().CheckIssuance(t.Context(), &database.Tx{}); err == nil {
				t.Fatal("issuance did not share model validation")
			}
		})
	}
	current, present = original, true
	if got, err := provider.RecheckPassword(t.Context(), &database.Tx{}, auth.PasswordResult[passwordSubject, int64]{}); err == nil || got != (passwordSubject{}) {
		t.Fatal("zero result accepted")
	}
	if _, err := provider.RecheckPassword(t.Context(), nil, result); err == nil {
		t.Fatal("nil transaction accepted")
	}
	if _, err := provider.RecheckPassword(nil, &database.Tx{}, result); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestPasswordRecheckOwnsAbnormalCallbacksAndCancellation(t *testing.T) {
	hasher := loginHasher(t, 2)
	plain := loginPlain(t, "private password")
	hash, err := hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	original := passwordSubject{ID: 7, Digest: hash, Enabled: true}
	cause := errors.New("lock failed")
	for _, mode := range []string{"error", "panic", "goexit", "cancel", "already-canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			binding := loginBinding(original)
			binding.Lock = func(context.Context, *database.Tx, passwordSubject) (value.Optional[passwordSubject], error) {
				calls++
				switch mode {
				case "error":
					return value.Optional[passwordSubject]{}, cause
				case "panic":
					panic("private panic")
				case "goexit":
					runtime.Goexit()
				case "cancel":
					cancel()
				}
				return value.Set(original), nil
			}
			provider := passwordProvider(func(context.Context, passwordSubject) (bool, error) { return true, nil })
			login, err := auth.NewPasswordLogin(provider, hasher, binding, auth.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			result, err := login.Authenticate(t.Context(), "member@example.test", plain)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "already-canceled" {
				cancel()
			}
			got, err := provider.RecheckPassword(ctx, &database.Tx{}, result)
			if err == nil || got != (passwordSubject{}) {
				t.Fatal("failed recheck returned model")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("cause lost")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
			if mode == "already-canceled" && calls != 0 {
				t.Fatal("callback ran after cancellation")
			}
		})
	}
}
