package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CreateDraft is the generated bridge for typed infrastructure that creates a
// model. Defaults fill omitted inputs before normal UUID preparation. Consumers
// pass their concrete generated draft; they do not construct mutation metadata.
type CreateDraft[M any] interface {
	FoundryCreateMutation(defaults Mutation[M]) (Mutation[M], error)
}

// MaxRelationWriteRows bounds the models retained by one atomic relation write.
// Relation operations share the ordinary atomic batch row budget.
const MaxRelationWriteRows = MaxPerModelWriteRows

// WithWriteLimit lowers the maximum links changed by Detach or ForceDetach.
// The limit must be positive and no greater than MaxRelationWriteRows. An excess
// fails before the first pivot mutation rather than silently truncating matches.
// It does not change eager loading, predicates or ordinary query limits.
func (r ThroughRelation[M, N, P]) WithWriteLimit(limit int) ThroughRelation[M, N, P] {
	r.writeLimit = value.Set(limit)
	return r
}

func (r ThroughRelation[M, N, P]) validateWrite() (int, error) {
	if err := r.validateRelation(r.spec.source.table, 0, DefaultRelationLimits()); err != nil {
		return 0, err
	}
	limit := MaxRelationWriteRows
	if selected, ok := r.writeLimit.Get(); ok {
		limit = selected
	}
	if err := validatePerModelWriteLimit(limit); err != nil {
		return 0, err
	}
	if len(r.orders) != 0 || len(r.spec.target.orders) != 0 || len(r.pivot.orders) != 0 ||
		len(r.spec.target.relations) != 0 || len(r.pivot.relations) != 0 ||
		r.spec.target.relationLimits != nil || r.pivot.relationLimits != nil {
		return 0, fault.New(fault.Invalid, "relation writes do not accept ordering or eager-loading options")
	}
	if r.pivotLocal == r.pivotForeign {
		return 0, fault.New(fault.Invalid, "relation writes require two distinct pivot key fields")
	}
	return limit, nil
}

// Attach creates one concrete pivot through its ordinary lifecycle pipeline.
// It refreshes both endpoint identities, derives their stored relation keys and
// fills omitted draft inputs. Explicit inputs and hooks may transform values,
// but the stored pivot must satisfy the relation's endpoints and filter scopes.
// Duplicate links are ordinary inserts: migrations own their uniqueness policy.
func (r ThroughRelation[M, N, P]) Attach(ctx context.Context, writer database.Transactor, source M, target N, draft CreateDraft[P]) (P, error) {
	if err := writeContext(ctx, writer); err != nil {
		return *new(P), err
	}
	if _, err := r.validateWrite(); err != nil {
		return *new(P), err
	}
	if nilDescriptor(draft) {
		return *new(P), fault.New(fault.Invalid, "relation attachment requires a typed pivot draft")
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) (P, error) {
		scoped, keys, err := r.writeEndpoints(ctx, tx, source, target)
		if err != nil {
			return *new(P), err
		}
		mutation, err := draft.FoundryCreateMutation(keys.defaults)
		if err != nil {
			return *new(P), err
		}
		// Creation has no read filters. The postcondition below evaluates all
		// relation filters on the actual stored result inside this transaction.
		pivot := ForModel(*scoped.pivot.definition)
		created, err := pivot.Insert(ctx, tx, mutation)
		if err != nil {
			return *new(P), err
		}
		primary, err := modelWritePredicate(pivot, created)
		if err != nil {
			return *new(P), err
		}
		matches, err := scoped.spec.source.WhereHas(scoped.WherePivot(primary)).Exists(ctx, tx)
		if err != nil {
			return *new(P), err
		}
		if !matches {
			return *new(P), fault.New(fault.Invalid, "stored pivot does not satisfy the relation endpoints and filters")
		}
		return created, nil
	})
}

// Detach removes every matching active pivot through normal per-model Delete
// behavior. A soft-delete pivot remains stored; an ordinary pivot is physically
// removed. Endpoint/filter scopes remain in force. Duplicate links are separate
// writes with separate hooks. All selected links commit or roll back together.
func (r ThroughRelation[M, N, P]) Detach(ctx context.Context, writer database.Transactor, source M, target N) ([]P, error) {
	return r.detach(ctx, writer, source, target, false)
}

// ForceDetach physically removes matching soft-delete pivots through their
// force-delete lifecycle. Pivot visibility is retained: use WithTrashedPivot to
// include deleted links. Ordinary pivots already use physical Detach behavior.
func (r ThroughRelation[M, N, P]) ForceDetach(ctx context.Context, writer database.Transactor, source M, target N) ([]P, error) {
	return r.detach(ctx, writer, source, target, true)
}

func (r ThroughRelation[M, N, P]) detach(ctx context.Context, writer database.Transactor, source M, target N, force bool) ([]P, error) {
	if err := writeContext(ctx, writer); err != nil {
		return nil, err
	}
	limit, err := r.validateWrite()
	if err != nil {
		return nil, err
	}
	if force && !r.pivot.hasSoftDeletes() {
		return nil, fault.New(fault.Invalid, "force detachment requires a soft-delete pivot")
	}
	return executeWrite(ctx, writer, func(ctx context.Context, tx *database.Tx) ([]P, error) {
		scoped, keys, err := r.writeEndpoints(ctx, tx, source, target)
		if err != nil {
			return nil, err
		}
		selected := scoped.pivot.Where(keys.predicates...)
		kind := forceDeleteModel
		if !force {
			selected, kind = selected.removal()
		}
		return mutateSelectedModels(ctx, tx, selected, kind, limit, nil)
	})
}
