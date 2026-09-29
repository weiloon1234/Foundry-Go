package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/lockout/memory"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"github.com/weiloon1234/Foundry-Go/value"
)

type enrolledFactors struct{ enrolled map[int64]bool }

func (f enrolledFactors) HasEnrolledFactor(_ context.Context, subject passwordSubject) (bool, error) {
	return f.enrolled[subject.ID], nil
}

func loginThrottle(t *testing.T, policy lockout.Policy) lockout.Throttle[loginEmail] {
	t.Helper()
	backend, err := memory.New(16, testkit.NewClock(time.Unix(1000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	store, err := lockout.NewStore(backend, lockout.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "links"}))
	if err != nil {
		t.Fatal(err)
	}
	throttle, err := lockout.DefineLogin("password", keyspace.StringKeys[loginEmail](), lockout.Limits{PerClient: policy, Account: policy, Address: value.Set(policy)}).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	return throttle
}

// An enrolled factor always requires MFA, even when the application's
// RequiresMFA callback forgot to mirror the factor's enabled state.
func TestEnrolledSecondFactorCannotBeBypassed(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "right password")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	b := loginBinding(passwordSubject{ID: 7, Digest: hash, Enabled: true})
	b.RequiresMFA = func(context.Context, passwordSubject) (bool, error) { return false, nil }
	login, err := loginInstance(t, h, b).WithSecondFactor(enrolledFactors{enrolled: map[int64]bool{7: true}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := login.Authenticate(t.Context(), "member@example.test", plain)
	if err != nil || result.Proof().Assurance() != auth.PendingMFA {
		t.Fatal("enrolled factor was bypassed", err)
	}
	if _, err := login.WithSecondFactor(enrolledFactors{}); err == nil {
		t.Fatal("second factor link replaced")
	}
}

// A password replaced between verification and the rehash write fails the
// attempt, but it was not a wrong guess and must not consume lockout budget.
func TestConcurrentPasswordChangeIsNotALockoutFailure(t *testing.T) {
	oldHasher, h := loginHasher(t, 2), loginHasher(t, 3)
	plain := loginPlain(t, "old password")
	old, err := oldHasher.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	next := loginPlain(t, "new password")
	replaced, err := h.Hash(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	current := passwordSubject{ID: 7, Digest: old, Enabled: true}
	b := loginBinding(current)
	b.Lookup = func(_ context.Context, key loginEmail) (value.Optional[passwordSubject], error) {
		if key != "member@example.test" {
			return value.Optional[passwordSubject]{}, nil
		}
		return value.Set(current), nil
	}
	b.Rehash = func(_ context.Context, subject passwordSubject, _, _ password.Hash) (value.Optional[passwordSubject], error) {
		current.Digest = replaced // A reset committed first.
		return value.Optional[passwordSubject]{}, nil
	}
	var events []auth.Event
	login, err := loginInstance(t, h, b).WithLockout(loginThrottle(t, lockout.Policy{MaxFailures: 1, Window: time.Minute, LockFor: time.Minute}))
	if err != nil {
		t.Fatal(err)
	}
	login, err = login.WithObserver(func(_ context.Context, event auth.Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	result, err := login.Authenticate(t.Context(), "member@example.test", plain)
	if !errors.Is(err, auth.Unauthenticated) || errors.Is(err, lockout.Locked) {
		t.Fatal("replaced password accepted or counted as a guess", err)
	}
	assertNoPasswordAuthority(t, result)
	if result, err := login.Authenticate(t.Context(), "member@example.test", next); err != nil || result.Proof().Assurance() != auth.Authenticated {
		t.Fatal("concurrent change consumed the lockout budget", err)
	}
	if len(events) != 1 || events[0].Kind != auth.EventFailed || !events[0].Subject.IsSet() {
		t.Fatal("unexpected login observations", len(events))
	}
	if _, err := login.Authenticate(t.Context(), "member@example.test", plain); !errors.Is(err, lockout.Locked) {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Kind != auth.EventLockout {
		t.Fatal("lockout was not observed", len(events))
	}
	// With a one-failure threshold the first miss also locks; the observation
	// never names a subject for an unknown submitted identifier.
	if _, err := login.Authenticate(t.Context(), "missing@example.test", plain); !errors.Is(err, lockout.Locked) {
		t.Fatal(err)
	}
	if len(events) != 3 || events[2].Kind != auth.EventLockout || events[2].Subject.IsSet() {
		t.Fatal("unknown account failure disclosed a subject", len(events))
	}
}

func TestPasswordConfirmationRechecksCurrentHash(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "right password")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	subject := passwordSubject{ID: 7, Digest: hash, Enabled: true}
	login := loginInstance(t, h, loginBinding(subject))
	if err := login.Confirm(t.Context(), subject, plain); err != nil {
		t.Fatal(err)
	}
	if err := login.Confirm(t.Context(), subject, loginPlain(t, "wrong password")); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if err := login.Confirm(t.Context(), passwordSubject{ID: 7}, plain); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("unusable hash confirmed", err)
	}
}

// Password confirmation is throttled per subject, so a stolen session cannot
// brute-force the password through a confirmation screen; other subjects are
// unaffected.
func TestPasswordConfirmationLockoutIsPerSubject(t *testing.T) {
	h := loginHasher(t, 2)
	plain := loginPlain(t, "right password")
	hash, err := h.Hash(t.Context(), plain)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := memory.New(16, testkit.NewClock(time.Unix(1000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	store, err := lockout.NewStore(backend, lockout.DefaultConfig(keyspace.Namespace{Application: "test", Environment: "confirm"}))
	if err != nil {
		t.Fatal(err)
	}
	throttle, err := lockout.Define("accounts.confirm", auth.ConfirmationKeys(), lockout.Policy{MaxFailures: 2, Window: time.Minute, LockFor: time.Minute}).Bind(store)
	if err != nil {
		t.Fatal(err)
	}
	victim, other := passwordSubject{ID: 7, Digest: hash, Enabled: true}, passwordSubject{ID: 8, Digest: hash, Enabled: true}
	login, err := loginInstance(t, h, loginBinding(victim)).WithConfirmationLockout(throttle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := login.WithConfirmationLockout(throttle); err == nil {
		t.Fatal("a second confirmation lockout was accepted")
	}
	wrong := loginPlain(t, "wrong password")
	if err := login.Confirm(t.Context(), victim, wrong); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal(err)
	}
	if err := login.Confirm(t.Context(), victim, wrong); !errors.Is(err, lockout.Locked) {
		t.Fatal("confirmation failures did not lock the subject", err)
	}
	if err := login.Confirm(t.Context(), victim, plain); !errors.Is(err, lockout.Locked) {
		t.Fatal("a locked subject confirmed", err)
	}
	if err := login.Confirm(t.Context(), other, plain); err != nil {
		t.Fatal("another subject was locked", err)
	}
}
