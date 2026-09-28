package query

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ScopedRelation is a generated direct relationship (BelongsTo, HasOne or
// HasMany) retaining both model owners. Through/pivot relationships do not
// implicitly acquire direct-key semantics.
type ScopedRelation[P, M any] interface{ lookupRelation() relationLookup[P, M] }
type relationLookup[P, M any] struct {
	spec     relationSpec[P, M]
	validate func() error
}

func (r OneRelation[P, M]) lookupRelation() relationLookup[P, M] {
	return relationLookup[P, M]{r.spec, func() error { return r.validateRelation(r.spec.source.table, 0, r.spec.target.loadLimits()) }}
}
func (r ManyRelation[P, M]) lookupRelation() relationLookup[P, M] {
	return relationLookup[P, M]{r.spec, func() error { return r.validateRelation(r.spec.source.table, 0, r.spec.target.loadLimits()) }}
}

// RelatedLookup keeps the declared relationship scope and unique target field.
// Find consumes an already loaded parent and never re-fetches it or performs a
// global target fallback. Matching the relationship is not authorization.
type RelatedLookup[P, M any, K comparable] struct {
	relation relationLookup[P, M]
	field    ScalarField[M, K]
	err      error
}

func RelatedUnique[P, M any, K comparable](relation ScopedRelation[P, M], field KeyField[M, K]) RelatedLookup[P, M, K] {
	if nilDescriptor(relation) || nilDescriptor(field) {
		return RelatedLookup[P, M, K]{err: fault.New(fault.Invalid, "related lookup requires a direct relation and stored target field")}
	}
	return RelatedLookup[P, M, K]{relation: relation.lookupRelation(), field: field.relationKey()}
}
func (l RelatedLookup[P, M, K]) Validate() error {
	if l.err != nil {
		return l.err
	}
	if l.relation.validate == nil || l.relation.spec.scope == nil {
		return fault.New(fault.Invalid, "related lookup requires a direct relationship")
	}
	if err := l.relation.validate(); err != nil {
		return err
	}
	return Unique(l.relation.spec.target, l.field).Validate()
}
func (l RelatedLookup[P, M, K]) Find(ctx context.Context, executor database.Executor, parent P, key K) (value.Optional[M], error) {
	if err := executionContext(ctx, executor); err != nil {
		return value.Optional[M]{}, err
	}
	if err := l.Validate(); err != nil {
		return value.Optional[M]{}, err
	}
	var source Query[M]
	var present bool
	var returned error
	spec := l.relation.spec
	failure := callback.Isolated("model relationship key", func() error {
		source, present, returned = spec.scope(ctx, spec.source, spec.target, parent)
		return nil
	})
	if failure != nil {
		return value.Optional[M]{}, fault.Wrap(fault.Internal, "model relationship scope failed", failure)
	}
	if err := ctx.Err(); err != nil {
		return value.Optional[M]{}, err
	}
	if returned != nil {
		return value.Optional[M]{}, returned
	}
	if !present {
		return value.Optional[M]{}, nil
	}
	return uniqueModel(ctx, executor, source.Where(l.field.Eq(key)))
}

// Format omits scoped values and internal model metadata from diagnostics.
func (RelatedLookup[P, M, K]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("related model lookup"))
}
