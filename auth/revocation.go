package auth

import (
	"context"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
)

const MaxRevocationTargets = 32

// RevocationName identifies one stable credential-store contribution.
type RevocationName string

// Revocation is a typed, explicitly registered credential-store contribution.
// Framework bindings supply stable names and their original provider declaration.
// A callback must join tx, use bounded work and never commit or retry. The returned
// count is provisional until the surrounding transaction commits.
type Revocation[M model.Identifiable, K any] struct {
	name     RevocationName
	provider Provider[M, K]
	revoke   func(context.Context, *database.Tx, model.Reference[M, K]) (uint64, error)
}

func DefineRevocation[M model.Identifiable, K any](name RevocationName, provider Provider[M, K], revoke func(context.Context, *database.Tx, model.Reference[M, K]) (uint64, error)) Revocation[M, K] {
	return Revocation[M, K]{name: name, provider: provider, revoke: revoke}
}
func (r Revocation[M, K]) Validate() error {
	if !identifier.Semantic(string(r.name)) || r.revoke == nil {
		return fault.New(fault.Invalid, "credential revocation requires a stable name and transaction callback")
	}
	return r.provider.Validate()
}
func (r Revocation[M, K]) Name() RevocationName { return r.name }

// Revocations groups all registered session/token guards for one model provider.
// It sorts stable target names once, rejects duplicates/provider mismatches and
// applies every contribution inside one savepoint. Consumers pass Invalidate
// directly to password reset or other credential-changing domain operations.
type Revocations[M model.Identifiable, K any] struct {
	provider Provider[M, K]
	targets  []Revocation[M, K]
}

func NewRevocations[M model.Identifiable, K any](provider Provider[M, K], targets ...Revocation[M, K]) (*Revocations[M, K], error) {
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	if len(targets) < 1 || len(targets) > MaxRevocationTargets {
		return nil, fault.New(fault.Invalid, "credential revocation requires a bounded nonempty set of stores")
	}
	targets = slices.Clone(targets)
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return nil, err
		}
		if target.provider.definition.id != provider.definition.id {
			return nil, fault.New(fault.Invalid, "revocation stores must share the same provider declaration")
		}
	}
	slices.SortFunc(targets, func(a, b Revocation[M, K]) int { return strings.Compare(string(a.name), string(b.name)) })
	for i := 1; i < len(targets); i++ {
		if targets[i].name == targets[i-1].name {
			return nil, fault.New(fault.Duplicate, "duplicate credential revocation store")
		}
	}
	return &Revocations[M, K]{provider: provider, targets: targets}, nil
}
func (r *Revocations[M, K]) Validate() error {
	if r == nil || len(r.targets) == 0 {
		return fault.New(fault.Invalid, "credential revocations are not configured")
	}
	return nil
}

// Invalidate uses the model's stored identity without checking eligibility: a
// disabled model must still be revocable. The caller must already hold the model
// lock and propagate this error. All registered targets use the same order;
// acquire no model locks after entering a credential-store contribution.
func (r *Revocations[M, K]) Invalidate(ctx context.Context, tx *database.Tx, subject M) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if ctx == nil || tx == nil {
		return fault.New(fault.Invalid, "credential invalidation requires a context and transaction")
	}
	return tx.Savepoint(ctx, func(child *database.Tx) error {
		identity, err := subject.FoundryIdentity()
		if err != nil {
			return err
		}
		reference, err := r.provider.Parse(identity)
		if err != nil {
			return err
		}
		for _, target := range r.targets {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := target.revoke(ctx, child, reference); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
}
