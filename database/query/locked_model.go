package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// LockedQuery reads complete models under a transaction-owned row lock. It is
// deliberately not an unrestricted query source or writer. Closing rows is not an unlock:
// locks end with their transaction or rollback of their owning savepoint.
type LockedQuery[M any] struct {
	query Query[M]
	lock  lockSpec
}

// ForUpdate requests an exclusive row lock, waiting for conflicting row locks.
func (q Query[M]) ForUpdate() LockedQuery[M] {
	return LockedQuery[M]{query: q, lock: lockSpec{strength: lockUpdate}}
}

// ForNoKeyUpdate requests a row lock compatible with FOR KEY SHARE.
func (q Query[M]) ForNoKeyUpdate() LockedQuery[M] {
	return LockedQuery[M]{query: q, lock: lockSpec{strength: lockNoKeyUpdate}}
}

// ForShare requests a shared row lock that prevents updates and deletion.
func (q Query[M]) ForShare() LockedQuery[M] {
	return LockedQuery[M]{query: q, lock: lockSpec{strength: lockShare}}
}

// ForKeyShare prevents deletion and key-changing updates to selected rows.
func (q Query[M]) ForKeyShare() LockedQuery[M] {
	return LockedQuery[M]{query: q, lock: lockSpec{strength: lockKeyShare}}
}

// NoWait reports a PostgreSQL lock error instead of waiting for a row lock.
func (q LockedQuery[M]) NoWait() LockedQuery[M] { q.lock.behavior = lockNoWait; return q }

// SkipLocked omits rows with conflicting locks. This is useful for queue claims,
// not a consistent view of all matching records. It replaces any prior NoWait.
func (q LockedQuery[M]) SkipLocked() LockedQuery[M] { q.lock.behavior = lockSkip; return q }

// Wait restores the default row-lock waiting policy; context cancellation applies.
func (q LockedQuery[M]) Wait() LockedQuery[M] { q.lock.behavior = lockWait; return q }

func (q LockedQuery[M]) Where(predicates ...Predicate[M]) LockedQuery[M] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q LockedQuery[M]) WhereHas(relation ExistenceRelation[M]) LockedQuery[M] {
	q.query = q.query.WhereHas(relation)
	return q
}
func (q LockedQuery[M]) WhereDoesntHave(relation ExistenceRelation[M]) LockedQuery[M] {
	q.query = q.query.WhereDoesntHave(relation)
	return q
}
func (q LockedQuery[M]) OrderBy(orders ...Order[M]) LockedQuery[M] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q LockedQuery[M]) Limit(count int) LockedQuery[M]  { q.query = q.query.Limit(count); return q }
func (q LockedQuery[M]) Offset(count int) LockedQuery[M] { q.query = q.query.Offset(count); return q }

// With eager-loads relations on the same transaction after closing parent rows.
// Only selected parent rows are locked; related reads do not inherit that lock.
func (q LockedQuery[M]) With(relations ...Relation[M]) LockedQuery[M] {
	q.query = q.query.With(relations...)
	return q
}
func (q LockedQuery[M]) WithRelationLimits(limits RelationLimits) LockedQuery[M] {
	q.query = q.query.WithRelationLimits(limits)
	return q
}

func (q LockedQuery[M]) reader() readResult[M] {
	r := readResult[M]{transaction: true}
	if r.err = q.query.Validate(); r.err != nil {
		return r
	}
	if q.query.definition == nil {
		r.err = fault.New(fault.Invalid, "locked model query requires generated metadata and hydration")
		return r
	}
	r.node, r.scan = q.query.modelSelect(), q.query.definition.scan
	r.lifecycle = q.query.definition.retrieval()
	r.node.locks = []lockSpec{q.lock}
	return r
}

// Compile validates the query and row-lock semantics without executing it.
func (q LockedQuery[M]) Compile() (Statement, error) { return q.reader().Compile() }

// All collects locked models and discards partial results on any failure.
// A nil transaction is rejected; a pool or session does not satisfy this API.
func (q LockedQuery[M]) All(ctx context.Context, tx *database.Tx) ([]M, error) {
	if err := lockContext(ctx, tx); err != nil {
		return nil, err
	}
	items, err := q.reader().All(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(q.query.relations) != 0 {
		return q.query.Load(ctx, tx, items)
	}
	return items, nil
}

// First locks at most one matching model, defaulting to primary-key order.
// It honors an explicit zero limit, filters and offsets.
func (q LockedQuery[M]) First(ctx context.Context, tx *database.Tx) (value.Optional[M], error) {
	q.query = q.query.firstQuery()
	items, err := q.All(ctx, tx)
	if err != nil {
		return value.Optional[M]{}, err
	}
	if len(items) == 0 {
		return value.Optional[M]{}, nil
	}
	return value.Set(items[0]), nil
}

// RequireFirst reports database.NotFound when no model can be selected.
func (q LockedQuery[M]) RequireFirst(ctx context.Context, tx *database.Tx) (M, error) {
	item, err := q.First(ctx, tx)
	if err != nil {
		return *new(M), err
	}
	if result, ok := item.Get(); ok {
		return result, nil
	}
	return *new(M), database.NotFound
}

// Each streams locked models. It requires no eager-loading clauses; use All for
// eager relations. A callback error closes rows without requesting rollback.
// Callbacks run with rows open and cannot execute another operation on this Tx.
// Drivers may drain unread rows on close; use Limit to bound the selected set.
func (q LockedQuery[M]) Each(ctx context.Context, tx *database.Tx, yield func(M) error) error {
	if err := lockContext(ctx, tx); err != nil {
		return err
	}
	if len(q.query.relations) != 0 || q.query.relationLimits != nil {
		return fault.New(fault.Invalid, "locked streaming cannot eager-load relations; use All")
	}
	return q.reader().Each(ctx, tx, yield)
}
