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
type Model[M model.Identifiable, K any] struct {
	Lock          func(context.Context, *database.Tx, K) (value.Optional[M], error)
	Email         func(M) string
	EmailRevision func(M) challenge.Revision[M]
	Verified      func(M) bool
	MarkVerified  func(context.Context, *database.Tx, M) (M, error)
}
type Verification[M model.Identifiable, K any] struct {
	flow  *challenge.Flow[M, K, challenge.EmailVerification]
	model Model[M, K]
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
	}}, lifetime)
	if err != nil {
		return nil, err
	}
	return &Verification[M, K]{flow: flow, model: binding}, nil
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
	return v.flow.Consume(ctx, token, func(op context.Context, tx *database.Tx, subject M) (M, error) {
		updated, err := v.model.MarkVerified(op, tx, subject)
		if err != nil {
			return *new(M), err
		}
		if !v.model.Verified(updated) || v.model.Email(updated) != v.model.Email(subject) || v.model.EmailRevision(updated) != v.model.EmailRevision(subject) {
			return *new(M), fault.New(fault.Invalid, "email verification changed email state or returned an unverified model")
		}
		return updated, nil
	})
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
