package auth

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

// concurrentPasswordChange marks an attempt whose password was correct when
// checked but was replaced before the rehash write. It is Unauthenticated for
// callers, yet lockout does not count it as a failed guess.
var concurrentPasswordChange = fault.New(fault.Conflict, "password changed during login")

// rehash preserves identity and confirms eligibility again only when a write
// returns a new model snapshot. It never retries an omitted or uncertain write.
func (l *PasswordLogin[M, K, I]) rehash(ctx context.Context, key I, subject M, identity model.Identity, stored password.Hash, plain password.Plaintext) (M, error) {
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
		return l.afterLostRehash(ctx, key, identity, plain)
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

// afterLostRehash handles a compare-and-swap lost to another writer. A parallel
// login usually won with a new hash of the same password: reload once and
// verify against the winning hash (one more bounded KDF, no write retry). A
// changed, missing or replaced account fails as a concurrent password change.
func (l *PasswordLogin[M, K, I]) afterLostRehash(ctx context.Context, key I, identity model.Identity, plain password.Plaintext) (M, error) {
	changed := Unauthenticated.WithCause(concurrentPasswordChange)
	found, err := l.model.Lookup(ctx, key)
	if err != nil {
		return *new(M), err
	}
	if err := ctx.Err(); err != nil {
		return *new(M), err
	}
	current, present := found.Get()
	if !present {
		return *new(M), changed
	}
	actual, err := current.FoundryIdentity()
	if err != nil {
		return *new(M), err
	}
	if actual != identity {
		return *new(M), changed
	}
	matched, err := l.hasher.Check(ctx, plain, l.model.Hash(current))
	if errors.Is(err, fault.Invalid) || err == nil && !matched {
		return *new(M), changed
	}
	if err != nil {
		return *new(M), err
	}
	if err := l.provider.checkEligibility(ctx, current); err != nil {
		return *new(M), err
	}
	return current, nil
}
