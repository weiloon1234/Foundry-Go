package orm

import (
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryRelationOrder validates a computed order against the actual relation target.
// Example: FoundryRelationOrder(r,o,sqlowner.Metadata()).
func FoundryRelationOrder[M, N any](r ExistenceRelation[M, N], o ormcore.Order, seal sqlowner.MetadataSeal) Order[N] {
	if !seal.Valid() {
		return Order[N]{err: invalid("invalid SQL owner capability")}
	}
	if nilInterface(r) {
		return Order[N]{err: invalid("nil relation descriptor")}
	}
	plan, err := relationPredicatePlan(r.relationPlan(nil), reflect.TypeFor[M](), reflect.TypeFor[N]())
	if err != nil {
		return Order[N]{err: err}
	}
	return relationExpressionOrder[N](plan.target, o)
}

// FoundryRelationPivotOrder validates both relation owners and the concrete pivot.
// Example: FoundryRelationPivotOrder(r,o,sqlowner.Metadata()).
func FoundryRelationPivotOrder[M, N, P any](r ThroughRelation[M, N, P], o ormcore.Order, seal sqlowner.MetadataSeal) Order[P] {
	if !seal.Valid() {
		return Order[P]{err: invalid("invalid SQL owner capability")}
	}
	plan, err := relationPredicatePlan(r.plan, reflect.TypeFor[M](), reflect.TypeFor[N]())
	if err != nil {
		return Order[P]{err: err}
	}
	if plan.join == nil || plan.join.pivot.table == nil || plan.join.pivot.table.typ != reflect.TypeFor[P]() || plan.slot.pivot != reflect.TypeFor[P]() {
		return Order[P]{err: invalid("relation pivot type does not match bound metadata")}
	}
	return relationExpressionOrder[P](plan.join.pivot.table, o)
}

func relationExpressionOrder[N any](owner *tableCore, o ormcore.Order) Order[N] {
	source := ormcore.Source{Scope: reflect.TypeFor[N](), Owners: map[string]any{owner.model.Table: owner}}
	err := errors.Join(o.Err, declarationCoreError(owner), ormcore.CheckReferenceSet(source, o.Refs, o.ReferenceFree))
	order := querycore.ModelOrder(o.Node)
	return Order[N]{owner: owner, core: order, err: err}
}
