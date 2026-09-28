package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// rehash preserves identity and confirms eligibility again only when a write
// returns a new model snapshot. It never retries an omitted or uncertain write.
func (l *PasswordLogin[M, K, I]) rehash(ctx context.Context, subject M, identity model.Identity, stored password.Hash, plain password.Plaintext) (M, error) {
	needed, err := l.hasher.NeedsRehash(stored)
	if err != nil {
		return *new(M), err
	}
	if !needed {
		return subject, nil
	}
	next, err := l.hasher.Hash(ctx, plain)
	if err != nil {
		return *new(M), err
	}
	updated, err := l.model.Rehash(ctx, subject, stored, next)
	if err != nil {
		return *new(M), err
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	subject, present := updated.Get()
	if !present {
		return *new(M), Unauthenticated
	}
	actual, err := subject.FoundryIdentity()
	if err != nil {
		return *new(M), err
	}
	if actual != identity || l.model.Hash(subject) != next {
		return *new(M), fault.New(fault.Invalid, "password rehash returned a different identity or hash")
	}
	if err := l.provider.checkEligibility(ctx, subject); err != nil {
		return *new(M), err
	}
	return subject, nil
}
