package challenge

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type member struct {
	ID      int64
	Email   string
	Enabled bool
}

func (m member) reference() model.Reference[member, int64] {
	return model.NewReference[member]("challenge_test_members", m.ID, codec.Signed[int64]())
}
func (m member) FoundryIdentity() (model.Identity, error) { return m.reference().Identity() }

type fakeBackend struct {
	Backend
	issue   func(context.Context, Address, model.Identity, Digest, time.Duration, func(context.Context, *database.Tx) (Binding, error)) (Record, error)
	consume func(context.Context, Address, Digest, func(context.Context, *database.Tx, Record) error) (value.Optional[Record], error)
}

func (b *fakeBackend) Issue(ctx context.Context, a Address, id model.Identity, h Digest, l time.Duration, fn func(context.Context, *database.Tx) (Binding, error)) (Record, error) {
	return b.issue(ctx, a, id, h, l, fn)
}
func (b *fakeBackend) Consume(ctx context.Context, a Address, h Digest, fn func(context.Context, *database.Tx, Record) error) (value.Optional[Record], error) {
	return b.consume(ctx, a, h, fn)
}
func setup(t *testing.T, backend Backend, subject *member) *Flow[member, int64, PasswordReset] {
	t.Helper()
	store, err := NewStore(backend, DefaultConfig(keyspace.Namespace{Application: "recovery-unit", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("members", member{}.reference(), func(context.Context, int64) (value.Optional[member], error) { panic("second model lookup") }, func(_ context.Context, m member) (bool, error) { return m.Enabled, nil })
	flow, err := New[member, int64, PasswordReset](store, provider, Model[member, int64]{
		Lock: func(_ context.Context, tx *database.Tx, id int64) (value.Optional[member], error) {
			if tx == nil {
				return value.Optional[member]{}, errors.New("missing transaction")
			}
			return value.Set(*subject), nil
		},
		Binding: func(m member) (Binding, error) { return Bind(secret.New(m.Email)) },
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}
func issueBackend(ctx context.Context, a Address, id model.Identity, h Digest, l time.Duration, fn func(context.Context, *database.Tx) (Binding, error)) (Record, error) {
	binding, err := fn(ctx, &database.Tx{})
	if err != nil {
		return Record{}, err
	}
	now, _ := temporal.NewDateTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	expiry, _ := now.Add(l)
	return Record{Address: a, Subject: id, Hash: h, Binding: binding, CreatedAt: now, ExpiresAt: expiry}, nil
}
func TestRecoveryPublishesNothingOnUncertainOrMalformedIssuance(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit", "wrong-hash", "wrong-subject", "wrong-binding", "duplicate-callback", "suppressed-duplicate", "missing-callback", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			m := member{ID: 1, Email: "one@example.test", Enabled: true}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cause := errors.New("uncertain write")
			backend := &fakeBackend{issue: func(op context.Context, a Address, id model.Identity, h Digest, l time.Duration, fn func(context.Context, *database.Tx) (Binding, error)) (Record, error) {
				if mode == "missing-callback" {
					return Record{}, nil
				}
				result, err := issueBackend(op, a, id, h, l, fn)
				if err != nil {
					return Record{}, err
				}
				switch mode {
				case "error":
					return result, cause
				case "panic":
					panic("private recovery value")
				case "goexit":
					runtime.Goexit()
				case "wrong-hash":
					result.Hash = Digest{}
				case "wrong-subject":
					result.Subject, _ = (member{ID: 2}).FoundryIdentity()
				case "wrong-binding":
					result.Binding, _ = Bind(secret.New("different"))
				case "duplicate-callback":
					_, err = fn(op, &database.Tx{})
					return Record{}, err
				case "suppressed-duplicate":
					_, _ = fn(op, &database.Tx{})
				case "cancel":
					cancel()
				}
				return result, nil
			}}
			flow := setup(t, backend, &m)
			issued, err := flow.Issue(ctx, m.reference())
			if mode == "cancel" {
				// A backend that returned a complete record committed it; a late
				// cancellation must not discard the committed link.
				if err != nil || issued.Token().Secret().IsZero() {
					t.Fatal("committed issuance was lost after cancellation", err)
				}
				return
			}
			if err == nil || !issued.Token().Secret().IsZero() {
				t.Fatal("bad backend published a credential")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
			if strings.Contains(err.Error(), "private recovery value") {
				t.Fatal("panic disclosed")
			}
		})
	}
}
func TestRecoveryChecksLockedIdentityEligibilityAndBindingBeforeAction(t *testing.T) {
	m := member{ID: 1, Email: "one@example.test", Enabled: true}
	backend := &fakeBackend{issue: issueBackend}
	flow := setup(t, backend, &m)
	issued, err := flow.Issue(t.Context(), m.reference())
	if err != nil {
		t.Fatal(err)
	}
	original := m
	identity, _ := m.FoundryIdentity()
	binding, _ := Bind(secret.New(m.Email))
	hash, _ := HashSecret(issued.Token().Secret())
	now, _ := temporal.NewDateTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	expiry, _ := now.Add(time.Hour)
	stored := Record{Address: flow.address, Subject: identity, Hash: hash, Binding: binding, CreatedAt: now, ExpiresAt: expiry}
	backend.consume = func(ctx context.Context, _ Address, _ Digest, fn func(context.Context, *database.Tx, Record) error) (value.Optional[Record], error) {
		if err := fn(ctx, &database.Tx{}, stored); err != nil {
			return value.Optional[Record]{}, err
		}
		return value.Set(stored), nil
	}
	for _, mode := range []string{"email", "disabled", "identity"} {
		t.Run(mode, func(t *testing.T) {
			m = original
			switch mode {
			case "email":
				m.Email = "changed@example.test"
			case "disabled":
				m.Enabled = false
			case "identity":
				m.ID = 2
			}
			called := false
			result, err := flow.Consume(t.Context(), issued.Token(), func(_ context.Context, _ *database.Tx, m member) (member, error) { called = true; return m, nil })
			if err == nil || called || result.ID != 0 {
				t.Fatal("invalid model reached action")
			}
		})
	}
	m = original
	result, err := flow.Consume(t.Context(), issued.Token(), func(_ context.Context, _ *database.Tx, m member) (member, error) { return m, nil })
	if err != nil || result.ID != 1 {
		t.Fatal("valid stored model rejected", err)
	}
}
func TestRecoveryRejectsMalformedOrMissingTokenWithoutAction(t *testing.T) {
	m := member{ID: 1, Email: "one@example.test", Enabled: true}
	calls := 0
	backend := &fakeBackend{consume: func(context.Context, Address, Digest, func(context.Context, *database.Tx, Record) error) (value.Optional[Record], error) {
		calls++
		return value.Optional[Record]{}, nil
	}}
	flow := setup(t, backend, &m)
	action := func(context.Context, *database.Tx, member) (member, error) {
		t.Fatal("missing token reached action")
		return member{}, nil
	}
	if _, err := flow.Consume(t.Context(), Token[member, PasswordReset]{}, action); !errors.Is(err, auth.Unauthenticated) || calls != 0 {
		t.Fatal("malformed token queried backend", err)
	}
	parsed, err := ParseToken[member, PasswordReset](secret.New(strings.Repeat("A", 43)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Consume(t.Context(), parsed, action); !errors.Is(err, auth.Unauthenticated) || calls != 1 {
		t.Fatal("missing token result", err)
	}
}

func TestRecoveryRejectsSuppressedConsumptionCallbackFailures(t *testing.T) {
	for _, mode := range []string{"repeat", "error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			subject := member{ID: 1, Email: "one@example.test", Enabled: true}
			backend := &fakeBackend{issue: issueBackend}
			flow := setup(t, backend, &subject)
			issued, err := flow.Issue(t.Context(), subject.reference())
			if err != nil {
				t.Fatal(err)
			}
			identity, _ := subject.FoundryIdentity()
			binding, _ := Bind(secret.New(subject.Email))
			hash, _ := HashSecret(issued.Token().Secret())
			now, _ := temporal.NewDateTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
			expiry, _ := now.Add(time.Hour)
			stored := Record{Address: flow.address, Subject: identity, Hash: hash, Binding: binding, CreatedAt: now, ExpiresAt: expiry}
			backend.consume = func(ctx context.Context, _ Address, _ Digest, fn func(context.Context, *database.Tx, Record) error) (value.Optional[Record], error) {
				tx := &database.Tx{}
				_ = fn(ctx, tx, stored)
				if mode == "repeat" {
					_ = fn(ctx, tx, stored)
				}
				return value.Set(stored), nil
			}
			calls := 0
			result, err := flow.Consume(t.Context(), issued.Token(), func(context.Context, *database.Tx, member) (member, error) {
				calls++
				switch mode {
				case "error":
					return member{}, errors.New("action failed")
				case "panic":
					panic("secret action input")
				case "goexit":
					runtime.Goexit()
				}
				return subject, nil
			})
			if err == nil || result.ID != 0 || calls != 1 {
				t.Fatal("backend suppressed a callback failure", err)
			}
		})
	}
}

// A flow-specific eligibility rule replaces the provider's login eligibility,
// so email verification can reach accounts that cannot log in yet, while
// identity checks and a false decision still reject.
func TestFlowEligibilityOverridesProviderEligibility(t *testing.T) {
	store, err := NewStore(&fakeBackend{issue: issueBackend}, DefaultConfig(keyspace.Namespace{Application: "recovery-unit", Environment: "test"}))
	if err != nil {
		t.Fatal(err)
	}
	provider := auth.DefineProvider("members", member{}.reference(), func(context.Context, int64) (value.Optional[member], error) { panic("second model lookup") }, func(_ context.Context, m member) (bool, error) { return m.Enabled, nil })
	unverified := member{ID: 7, Email: "new@example.test", Enabled: false}
	allow := true
	flow, err := New[member, int64, EmailVerification](store, provider, Model[member, int64]{
		Lock: func(context.Context, *database.Tx, int64) (value.Optional[member], error) {
			return value.Set(unverified), nil
		},
		Binding:  func(m member) (Binding, error) { return Bind(secret.New(m.Email)) },
		Eligible: func(context.Context, member) (bool, error) { return allow, nil },
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if issued, err := flow.Issue(t.Context(), unverified.reference()); err != nil || issued.Token().Secret().IsZero() {
		t.Fatal("flow eligibility did not replace provider eligibility", err)
	}
	allow = false
	if _, err := flow.Issue(t.Context(), unverified.reference()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("ineligible model received a link", err)
	}
	defaulted := setup(t, &fakeBackend{issue: issueBackend}, &unverified)
	if _, err := defaulted.Issue(t.Context(), unverified.reference()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("provider eligibility no longer applies by default", err)
	}
}
