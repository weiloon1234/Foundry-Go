package query

import "github.com/weiloon1234/Foundry-Go/fault"

type ofManyChoice uint8

const (
	singleTarget ofManyChoice = iota
	orderedOfMany
	latestOfMany
	oldestOfMany
)

// OfMany turns a HasOne into "one of many", like Laravel's ofMany: among the
// targets matching the relationship's filters and scopes, each parent loads
// the first in these orders, with the target primary key breaking ties. It is
// loaded in one DISTINCT ON query per key batch, keyed by the parent (for
// HasOneThrough, the intermediate's first key). Filters take part in the
// choice, so HasOne(...).Where(paid).LatestOfMany() is the latest paid target
// and WhereHas matches when any filtered target exists. Relation aggregates
// over a one-of-many relationship are rejected.
func (r OneRelation[M, N]) OfMany(orders ...Order[N]) OneRelation[M, N] {
	r.spec.target = r.spec.target.OrderBy(orders...)
	r.ofMany = orderedOfMany
	return r
}

// LatestOfMany loads the target with the highest primary key per parent.
func (r OneRelation[M, N]) LatestOfMany() OneRelation[M, N] { r.ofMany = latestOfMany; return r }

// OldestOfMany loads the target with the lowest primary key per parent.
func (r OneRelation[M, N]) OldestOfMany() OneRelation[M, N] { r.ofMany = oldestOfMany; return r }

// oneOfManyTarget adds the per-key choice to the loaded target query.
func (r OneRelation[M, N]) oneOfManyTarget() Query[N] {
	target := r.spec.target
	if r.ofMany == singleTarget || target.definition == nil {
		return target
	}
	if r.ofMany != orderedOfMany {
		target = target.OrderBy(Order[N]{field: fieldRef{target.table, target.definition.primary}, descending: r.ofMany == latestOfMany})
	}
	// Choose per parent key: the target key for direct relations, the
	// intermediate's first key for HasOneThrough.
	key := r.spec.foreign
	if r.spec.hop != nil {
		key = fieldRef{hopAlias, r.spec.hop.first.column}
	}
	target.distinctOnKey = &key
	return target
}

func (r OneRelation[M, N]) validateOfMany() error {
	if r.ofMany > oldestOfMany {
		return fault.New(fault.Invalid, "invalid one-of-many choice")
	}
	if r.ofMany == orderedOfMany && len(r.spec.target.orders) == 0 {
		return fault.New(fault.Invalid, "one-of-many requires at least one order")
	}
	return nil
}
