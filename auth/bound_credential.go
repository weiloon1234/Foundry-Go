package auth

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/value"
)

// binderID identifies one BindStrategy call. Nonzero size: independent binders
// cannot alias.
type binderID struct{ marker byte }

// BoundCredential stands in for a credential that a trusted boundary has
// already exchanged, such as a redeemed single-use handshake ticket. Transport
// capture never creates one: only the Binder returned with a strategy can, and
// only that strategy verifies it, so request input can never present one. Every
// new scope re-verifies it, observing revocation and current eligibility like
// any other credential. Formatting never reveals its contents.
type BoundCredential struct {
	binder *binderID
	value  any
}

func (BoundCredential) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("bound authentication credential"))
}

// IsZero reports an absent bound credential.
func (b BoundCredential) IsZero() bool { return b.binder == nil }

// Binder creates bound credentials that only its own strategy verifies.
type Binder[B any] struct{ id *binderID }

func (b Binder[B]) Bind(bound B) (BoundCredential, error) {
	if b.id == nil {
		return BoundCredential{}, fault.New(fault.Invalid, "credential binder is not defined")
	}
	return BoundCredential{binder: b.id, value: bound}, nil
}

// BindStrategy extends a strategy to verify the bound credentials its returned
// Binder creates. Request secrets are verified exactly as before. A bound
// credential from any other binder is unauthenticated.
func BindStrategy[M, K, B any](strategy Strategy[M, K], verify func(context.Context, B) (value.Optional[Proof[M, K]], error)) (Strategy[M, K], Binder[B]) {
	id := &binderID{}
	strategy.bindable = true
	if verify != nil {
		strategy.bound = func(ctx context.Context, credential BoundCredential) (value.Optional[Proof[M, K]], error) {
			bound, ok := credential.value.(B)
			if credential.binder != id || !ok {
				return value.Optional[Proof[M, K]]{}, Unauthenticated
			}
			return verify(ctx, bound)
		}
	}
	return strategy, Binder[B]{id: id}
}

// WithBound returns a copy that also carries a bound credential for one
// declared source. A source carries either a transport secret or a bound
// credential, never both, so an ambiguous combination fails instead.
func (c Credentials) WithBound(name CredentialName, bound BoundCredential) (Credentials, error) {
	if !identifier.Semantic(string(name)) || bound.IsZero() {
		return Credentials{}, fault.New(fault.Invalid, "invalid bound authentication credential")
	}
	if !c.values[name].IsZero() || !c.bound[name].IsZero() {
		return Credentials{}, fault.New(fault.Duplicate, "authentication credential source is repeated")
	}
	if len(c.values)+len(c.bound) >= MaxCredentials {
		return Credentials{}, fault.New(fault.Invalid, "too many authentication credential sources")
	}
	bounds := make(map[CredentialName]BoundCredential, len(c.bound)+1)
	for source, credential := range c.bound {
		bounds[source] = credential
	}
	bounds[name] = bound
	// Secret values are never mutated after construction, so sharing them is safe.
	return Credentials{values: c.values, bound: bounds}, nil
}
