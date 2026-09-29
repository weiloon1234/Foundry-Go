// Package emailverification binds one-time verification to a concrete model's
// current stored email. Verifying an email never issues an authentication proof.
package emailverification

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

const DefaultLifetime = 24 * time.Hour

type Token[M any] = challenge.Token[M, challenge.EmailVerification]
type Issued[M any] = challenge.Issued[M, challenge.EmailVerification]

func ParseToken[M any](raw secret.String) (Token[M], error) {
	return challenge.ParseToken[M, challenge.EmailVerification](raw)
}

// Model maps stored email state. EmailRevision must be persisted and replaced
// transactionally whenever Email changes; restoring an old address must never
// restore its old revision. MarkVerified must preserve email and revision.
// Eligible optionally replaces the provider's eligibility for this flow, for
// example when the provider only admits accounts with verified addresses.
type Model[M model.Identifiable, K any] struct {
	Lock          func(context.Context, *database.Tx, K) (value.Optional[M], error)
	Email         func(M) string
	EmailRevision func(M) challenge.Revision[M]
	Verified      func(M) bool
	MarkVerified  func(context.Context, *database.Tx, M) (M, error)
	Eligible      func(context.Context, M) (bool, error)
}
type Verification[M model.Identifiable, K any] struct {
	flow     *challenge.Flow[M, K, challenge.EmailVerification]
	model    Model[M, K]
	provider auth.ProviderName
	observer auth.Observer
}

// WithObserver reports EventVerified after each committed verification.
func (v *Verification[M, K]) WithObserver(observer auth.Observer) (*Verification[M, K], error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if observer == nil {
		return nil, fault.New(fault.Invalid, "email verification observer is nil")
	}
	if v.observer != nil {
		return nil, fault.New(fault.Duplicate, "email verification observer already configured")
	}
	next := *v
	next.observer = observer
	return &next, nil
}

func New[M model.Identifiable, K any](store *challenge.Store, provider auth.Provider[M, K], binding Model[M, K], lifetime time.Duration) (*Verification[M, K], error) {
	if binding.Lock == nil || binding.Email == nil || binding.EmailRevision == nil || binding.Verified == nil || binding.MarkVerified == nil {
		return nil, fault.New(fault.Invalid, "email verification requires model callbacks")
	}
	flow, err := challenge.New[M, K, challenge.EmailVerification](store, provider, challenge.Model[M, K]{Lock: binding.Lock, Binding: func(subject M) (challenge.Binding, error) {
		if binding.Verified(subject) {
			return challenge.Binding{}, auth.Unauthenticated
		}
		return challenge.BindRevision(binding.EmailRevision(subject), secret.New(binding.Email(subject)))
	}, Eligible: binding.Eligible}, lifetime)
	if err != nil {
		return nil, err
	}
	return &Verification[M, K]{flow: flow, model: binding, provider: provider.Name()}, nil
}
func (v *Verification[M, K]) Validate() error {
	if v == nil || v.flow == nil {
		return fault.New(fault.Invalid, "email verification is not configured")
	}
	return v.flow.Validate()
}

// Issue uses the model's currently locked email for binding. Applications deliver
// only to the issued Subject's stored email and use a uniform public response.
func (v *Verification[M, K]) Issue(ctx context.Context, reference model.Reference[M, K]) (Issued[M], error) {
	if err := v.Validate(); err != nil {
		return Issued[M]{}, err
	}
	return v.flow.Issue(ctx, reference)
}
func (v *Verification[M, K]) Complete(ctx context.Context, token Token[M]) (M, error) {
	if err := v.Validate(); err != nil {
		return *new(M), err
	}
	verified, err := v.flow.Consume(ctx, token, func(op context.Context, tx *database.Tx, subject M) (M, error) {
		updated, err := v.model.MarkVerified(op, tx, subject)
		if err != nil {
			return *new(M), err
		}
		if !v.model.Verified(updated) || v.model.Email(updated) != v.model.Email(subject) || v.model.EmailRevision(updated) != v.model.EmailRevision(subject) {
			return *new(M), fault.New(fault.Invalid, "email verification changed email state or returned an unverified model")
		}
		return updated, nil
	})
	if err != nil {
		return *new(M), err
	}
	if v.observer != nil {
		if identity, err := verified.FoundryIdentity(); err == nil {
			auth.Notify(ctx, v.observer, auth.Event{Kind: auth.EventVerified, Provider: v.provider, Subject: value.Set(identity)})
		}
	}
	return verified, nil
}
func (v *Verification[M, K]) Revoke(ctx context.Context, reference model.Reference[M, K]) (bool, error) {
	if err := v.Validate(); err != nil {
		return false, err
	}
	return v.flow.Revoke(ctx, reference)
}
func (v *Verification[M, K]) Prune(ctx context.Context, limit int) (uint64, error) {
	if err := v.Validate(); err != nil {
		return 0, err
	}
	return v.flow.Prune(ctx, limit)
}
