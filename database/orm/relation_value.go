package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryRelatedValue retains validated relation ownership in a row expression.
// Example: FoundryRelatedValue(source,expression,column,sqlowner.Expressions()).
func FoundryRelatedValue[M, N, V any](source AggregateSource[M, N], expression ormcore.Expression, column ormcore.Column[V], seal sqlowner.ExpressionSeal) ormcore.Expression {
	computation := FoundryRelated(source, expression, column, seal)
	p := computation.plan
	result := ormcore.Expression{Err: p.err, Sensitive: expression.Sensitive || column.Sensitive}
	if result.Err != nil || p.aggregate == nil {
		return result
	}
	a := p.aggregate
	r := a.input.plan
	if r == nil || r.slot == nil || r.slot.owner == nil || r.slot.owner.typ != reflect.TypeFor[M]() {
		result.Err = invalid("related value requires its source model declaration")
		return result
	}
	result.Err = validateAggregateInput(a.input, r.slot.owner)
	result.Refs = []ormcore.Reference{{Scope: reflect.TypeFor[M](), Table: r.local.Table, Owner: r.slot.owner}}
	if result.Err != nil {
		return result
	}
	node, alias, group, _ := aggregateInputSelect(a, nil)
	node, err := querycore.RelatedValueSelect(node, aggregateOwner(a.input).model.Table, alias, group, r.local, a.measure)
	subquery := querycore.RelationExistence(node, r.local.Table, r.slot.owner.model.Columns)
	subquery.Err = err
	subquery.RelationCheck = func() error {
		return safeerror.Join("operation failed", validateAggregateInput(a.input, r.slot.owner), validateRelationMetadata(r, r.slot.owner))
	}
	result.Node = querycore.ScalarSubquery{Query: subquery}
	result.Err = err
	return result
}
