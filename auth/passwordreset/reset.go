// Package passwordreset provides typed model-first password recovery. The
// challenge and model write commit together; resetting never signs a user in.
package passwordreset

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
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
type Model[M model.Identifiable, K any] struct {
	Lock             func(context.Context, *database.Tx, K) (value.Optional[M], error)
	Email            func(M) string
	EmailRevision    func(M) challenge.Revision[M]
	Hash             func(M) password.Hash
	SetPassword      func(context.Context, *database.Tx, M, password.Hash) (M, error)
	ValidatePassword func(context.Context, password.Plaintext) error
	Invalidate       func(context.Context, *database.Tx, M) error
}

type Reset[M model.Identifiable, K any] struct {
	flow   *challenge.Flow[M, K, challenge.PasswordReset]
	hasher *password.Hasher
	model  Model[M, K]
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
	}}, lifetime)
	if err != nil {
		return nil, err
	}
	return &Reset[M, K]{flow: flow, hasher: hasher, model: binding}, nil
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
