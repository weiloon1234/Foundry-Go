package auth_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPasswordLoginLockoutPrecedesLookupAndProtectsProofPublication(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "correct input")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	b := loginBinding(passwordSubject{ID: 7, Digest: hash, Enabled: true})
	lookup := b.Lookup
	loads := 0
	b.Lookup = func(ctx context.Context, key loginEmail) (value.Optional[passwordSubject], error) {
		loads++
		return lookup(ctx, key)
	}
	clock := testkit.NewClock(time.Unix(1000, 0))
	backend, err := memory.New(10, clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	store, err := lockout.NewStore(backend, lockout.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "password"}))
	if err != nil {
		t.Fatal(err)
	}
	policy := lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: time.Second}
	single, err := lockout.Define("password.single", keyspace.StringKeys[loginEmail](), policy).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	// Submitted identifiers need client-aware limits; a single-key throttle
	// would let any client keep the account locked for everyone.
	if _, err := loginInstance(t, h, b).WithLockout(single); !errors.Is(err, fault.Invalid) {
		t.Fatal("single-key password lockout accepted", err)
	}
	throttle, err := lockout.DefineLogin("password", keyspace.StringKeys[loginEmail](), lockout.Limits{PerClient: policy, Account: policy, Address: value.Set(policy)}).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	login, err := loginInstance(t, h, b).WithLockout(throttle)
	if err != nil {
		t.Fatal(err)
	}
	bad := loginPlain(t, "wrong input")
	result, err := login.Authenticate(t.Context(), "member@example.test", bad)
	if !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	assertNoPasswordAuthority(t, result)
	result, err = login.Authenticate(t.Context(), "member@example.test", bad)
	if !errors.Is(err, lockout.Locked) {
		t.Fatal(err)
	}
	assertNoPasswordAuthority(t, result)
	result, err = login.Authenticate(t.Context(), "member@example.test", plain)
	if !errors.Is(err, lockout.Locked) || loads != 2 {
		t.Fatal("locked correct password reached lookup", loads, err)
	}
	assertNoPasswordAuthority(t, result)
	clock.Advance(time.Second)
	result, err = login.Authenticate(t.Context(), "member@example.test", plain)
	if err != nil || result.Proof().Assurance() != auth.Authenticated || loads != 3 {
		t.Fatal("unlocked password failed", err)
	}
	// Context cancellation after the primary factor's policy callback cannot leak
	// a proof or clear protection state through a detached finish operation.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b.RequiresMFA = func(context.Context, passwordSubject) (bool, error) { cancel(); return false, nil }
	canceled, err := loginInstance(t, h, b).WithLockout(throttle)
	if err != nil {
		t.Fatal(err)
	}
	result, err = canceled.Authenticate(ctx, "member@example.test", plain)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertNoPasswordAuthority(t, result)
}

type cyclicPasswordLookupError struct{ visits atomic.Int32 }

func (*cyclicPasswordLookupError) Error() string { panic("private login error must not be formatted") }
func (e *cyclicPasswordLookupError) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}
func TestCyclicPasswordFailureRetainsNoAuthorityOrFailedAttempt(t *testing.T) {
	hasher := loginHasher(t, 2)
	plain := loginPlain(t, "correct input")
	hash, err := hasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	binding := loginBinding(passwordSubject{ID: 7, Digest: hash, Enabled: true})
	lookup := binding.Lookup
	cycle := new(cyclicPasswordLookupError)
	fail := true
	binding.Lookup = func(ctx context.Context, key loginEmail) (value.Optional[passwordSubject], error) {
		if fail {
			return value.Optional[passwordSubject]{}, cycle
		}
		return lookup(ctx, key)
	}
	backend, err := memory.New(10, testkit.NewClock(time.Unix(1000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	protection := lockout.DefaultConfig(keyspace.Namespace{Application: "password-error-graph", Environment: "test"})
	protection.MaxConcurrent = 1
	store, err := lockout.NewStore(backend, protection)
	if err != nil {
		t.Fatal(err)
	}
	policy := lockout.Policy{MaxFailures: 1, Window: time.Minute, LockFor: time.Minute}
	throttle, err := lockout.DefineLogin("password", keyspace.StringKeys[loginEmail](), lockout.Limits{PerClient: policy, Account: policy, Address: value.Set(policy)}).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	config := auth.DefaultConfig()
	config.MaxConcurrent = 1
	login, err := loginInstance(t, hasher, binding, config).WithLockout(throttle)
	if err != nil {
		t.Fatal(err)
	}
	result, err := login.Authenticate(t.Context(), "member@example.test", plain)
	if err == nil || cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("cyclic login inspection did not finish safely")
	}
	assertNoPasswordAuthority(t, result)
	fail = false
	result, err = login.Authenticate(t.Context(), "member@example.test", plain)
	if err != nil || result.Proof().Assurance() != auth.Authenticated {
		t.Fatal("operational failure consumed a failed attempt or retained capacity")
	}
	result, err = login.Authenticate(t.Context(), "member@example.test", loginPlain(t, "wrong input"))
	if !errors.Is(err, lockout.Locked) {
		t.Fatal("confirmed password rejection no longer applies protection")
	}
	assertNoPasswordAuthority(t, result)
}
