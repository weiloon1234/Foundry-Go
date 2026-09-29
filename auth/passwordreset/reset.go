// Package passwordreset provides typed model-first password recovery. The
// challenge and model write commit together; resetting never signs a user in.
package passwordreset

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

const DefaultLifetime = time.Hour

type Token[M any] = challenge.Token[M, challenge.PasswordReset]
type Issued[M any] = challenge.Issued[M, challenge.PasswordReset]

func ParseToken[M any](raw secret.String) (Token[M], error) {
	return challenge.ParseToken[M, challenge.PasswordReset](raw)
}

// Model describes domain behavior using stored fields and generated mutations.
// Lock uses ForUpdate on tx. SetPassword must return the full updated model.
// ValidatePassword owns domain strength rules, not encoding/hashing policy.
// Invalidate MUST invalidate applicable credentials in the SAME transaction;
// failure rolls back the password change and link consumption. Do not call a
// store method that opens another transaction or defer revocation to after-commit.
// EmailRevision reads a nonzero persisted generation. Model lifecycle behavior
// must replace it whenever the stored email changes, even if an old email is
// restored later. Do not rotate it merely when issuing a link or resetting a
// password: verification links for an unchanged email remain independent.
// Additional outbox/audit writes may use tx. These callbacks must not send email.
// Eligible optionally replaces the provider's eligibility for this flow.
type Model[M model.Identifiable, K any] struct {
	Lock             func(context.Context, *database.Tx, K) (value.Optional[M], error)
	Email            func(M) string
	EmailRevision    func(M) challenge.Revision[M]
	Hash             func(M) password.Hash
	SetPassword      func(context.Context, *database.Tx, M, password.Hash) (M, error)
	ValidatePassword func(context.Context, password.Plaintext) error
	Invalidate       func(context.Context, *database.Tx, M) error
	Eligible         func(context.Context, M) (bool, error)
}

type Reset[M model.Identifiable, K any] struct {
	flow     *challenge.Flow[M, K, challenge.PasswordReset]
	hasher   *password.Hasher
	model    Model[M, K]
	provider auth.ProviderName
	unlock   func(context.Context, M) error
	observer auth.Observer
}

// WithLockout returns a reset view that clears the login throttle for the
// reset account after the reset commits, using key to derive the same login
// key the PasswordLogin uses (for example the stored email). It clears the
// account ceiling and the resetting client's window. Clearing is best effort:
// the committed reset is never undone, and a lockout backend failure leaves the
// old windows to expire on their own.
func WithLockout[M model.Identifiable, K, I any](reset *Reset[M, K], throttle lockout.Throttle[I], key func(M) I) (*Reset[M, K], error) {
	if err := reset.Validate(); err != nil {
		return nil, err
	}
	if err := throttle.Validate(); err != nil {
		return nil, err
	}
	if key == nil {
		return nil, fault.New(fault.Invalid, "password reset lockout requires a login-key mapping")
	}
	if reset.unlock != nil {
		return nil, fault.New(fault.Duplicate, "password reset already clears a lockout")
	}
	next := *reset
	next.unlock = func(ctx context.Context, subject M) error {
		var login I
		if err := callback.Isolated("password reset login key", func() error { login = key(subject); return nil }); err != nil {
			return err
		}
		_, err := throttle.Reset(ctx, login)
		return err
	}
	return &next, nil
}

// WithObserver reports EventPasswordReset after each committed reset.
func (r *Reset[M, K]) WithObserver(observer auth.Observer) (*Reset[M, K], error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if observer == nil {
		return nil, fault.New(fault.Invalid, "password reset observer is nil")
	}
	if r.observer != nil {
		return nil, fault.New(fault.Duplicate, "password reset observer already configured")
	}
	next := *r
	next.observer = observer
	return &next, nil
}

func New[M model.Identifiable, K any](store *challenge.Store, provider auth.Provider[M, K], hasher *password.Hasher, binding Model[M, K], lifetime time.Duration) (*Reset[M, K], error) {
	if err := hasher.Validate(); err != nil {
		return nil, err
	}
	if binding.Lock == nil || binding.Email == nil || binding.EmailRevision == nil || binding.Hash == nil || binding.SetPassword == nil || binding.ValidatePassword == nil || binding.Invalidate == nil {
		return nil, fault.New(fault.Invalid, "password reset requires model, password policy and transactional invalidation callbacks")
	}
	flow, err := challenge.New[M, K, challenge.PasswordReset](store, provider, challenge.Model[M, K]{Lock: binding.Lock, Binding: func(subject M) (challenge.Binding, error) {
		hash := binding.Hash(subject)
		if err := hash.Validate(); err != nil {
			return challenge.Binding{}, auth.Unauthenticated
		}
		return challenge.BindRevision(binding.EmailRevision(subject), secret.New(binding.Email(subject)), hash.Encoded())
	}, Eligible: binding.Eligible}, lifetime)
	if err != nil {
		return nil, err
	}
	return &Reset[M, K]{flow: flow, hasher: hasher, model: binding, provider: provider.Name()}, nil
}
func (r *Reset[M, K]) Validate() error {
	if r == nil || r.flow == nil {
		return fault.New(fault.Invalid, "password reset is not configured")
	}
	return r.flow.Validate()
}
func (r *Reset[M, K]) Issue(ctx context.Context, reference model.Reference[M, K]) (Issued[M], error) {
	if err := r.Validate(); err != nil {
		return Issued[M]{}, err
	}
	return r.flow.Issue(ctx, reference)
}

// Complete checks the link and current model before expensive hashing. The
// bounded KDF runs while the model is locked, avoiding a gap between validation
// and replacement. Apply request throttling before this operation. It returns
// no authentication proof; MFA requirements remain unchanged.
func (r *Reset[M, K]) Complete(ctx context.Context, token Token[M], plain password.Plaintext) (M, error) {
	if err := r.Validate(); err != nil {
		return *new(M), err
	}
	updated, err := r.consume(ctx, token, plain)
	if err != nil {
		return *new(M), err
	}
	if r.unlock != nil {
		// Best effort after commit: failure cannot undo the reset.
		_ = r.unlock(ctx, updated)
	}
	if r.observer != nil {
		if identity, err := updated.FoundryIdentity(); err == nil {
			auth.Notify(ctx, r.observer, auth.Event{Kind: auth.EventPasswordReset, Provider: r.provider, Subject: value.Set(identity)})
		}
	}
	return updated, nil
}
func (r *Reset[M, K]) consume(ctx context.Context, token Token[M], plain password.Plaintext) (M, error) {
	return r.flow.Consume(ctx, token, func(op context.Context, tx *database.Tx, subject M) (M, error) {
		if err := plain.Validate(); err != nil {
			return *new(M), err
		}
		if err := r.model.ValidatePassword(op, plain); err != nil {
			return *new(M), err
		}
		next, err := r.hasher.Hash(op, plain)
		if err != nil {
			return *new(M), err
		}
		updated, err := r.model.SetPassword(op, tx, subject, next)
		if err != nil {
			return *new(M), err
		}
		if r.model.Hash(updated) != next || r.model.Email(updated) != r.model.Email(subject) || r.model.EmailRevision(updated) != r.model.EmailRevision(subject) {
			return *new(M), fault.New(fault.Invalid, "password reset changed email state or returned a different password hash")
		}
		// Flow verifies model identity before committing. Do so before invalidation as
		// well, so a faulty setter cannot target another account's credentials.
		before, err := subject.FoundryIdentity()
		if err != nil {
			return *new(M), err
		}
		after, err := updated.FoundryIdentity()
		if err != nil {
			return *new(M), err
		}
		if before != after {
			return *new(M), fault.New(fault.Invalid, "password reset changed model identity")
		}
		if err := r.model.Invalidate(op, tx, updated); err != nil {
			return *new(M), err
		}
		return updated, nil
	})
}
func (r *Reset[M, K]) Revoke(ctx context.Context, reference model.Reference[M, K]) (bool, error) {
	if err := r.Validate(); err != nil {
		return false, err
	}
	return r.flow.Revoke(ctx, reference)
}
func (r *Reset[M, K]) Prune(ctx context.Context, limit int) (uint64, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	return r.flow.Prune(ctx, limit)
}
