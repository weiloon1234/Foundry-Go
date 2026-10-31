package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FromModel adapts a model query to a complete read-only model result/source.
// Scopes remain applied; eager-loading requests require explicit collection loading.
// Example: rows.Read(rows.Distinct(rows.FromModel(Users.Query()))).All(ctx,db).
func FromModel[M any](q orm.Query[M]) Query[M, M] {
	source, decode := orm.FoundryModel(q, sqlowner.Metadata())
	return Query[M, M]{source: source, decode: decode, recordOwner: source.Owners[source.Node.Source.Name()]}
}
func (q Query[S, R]) rowSource(*S) ormcore.Source { return q.source }

// GroupBy appends row-phase grouping keys. Example: rows.GroupBy(q,expr.Group(buyer)).
func GroupBy[S, R any](q Query[S, R], keys ...expr.GroupKey[S]) Query[S, R] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, key := range keys {
		inputs[i] = expr.FoundryGroup(key, sqlowner.Expressions())
	}
	q.source = ormcore.GroupBy(q.source, inputs)
	return q
}

// Having appends group conditions. Example: rows.Having(q,expr.GtValue(count, 0)).
func Having[S, R any](q Query[S, R], predicates ...expr.HavingPredicate[S]) Query[S, R] {
	inputs := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		inputs[i] = expr.FoundryHaving(p, sqlowner.Expressions())
	}
	q.source = ormcore.Having(q.source, inputs)
	return q
}

// OrderBy accepts row and selected ordering through this read-only boundary.
// Example: rows.OrderBy(q,expr.DescValue(count),expr.Asc(buyer)).
func OrderBy[S, R any](q Query[S, R], orders ...expr.ProjectionOrder[S]) Query[S, R] {
	inputs := make([]ormcore.Order, len(orders))
	for i, o := range orders {
		inputs[i] = expr.FoundryProjectionOrder(o, sqlowner.Expressions())
	}
	q.source = ormcore.OrderBy(q.source, inputs)
	return q
}

// Distinct removes duplicate results and replaces earlier DISTINCT ON.
// Example: rows.Distinct(rows.FromModel(Users.Query())).
func Distinct[S, R any](q Query[S, R]) Query[S, R] {
	q.source = ormcore.Distinct(q.source, querycore.DistinctRows, nil)
	return q
}

// DistinctOn selects one result per row-key combination; order by keys first.
// Example: rows.DistinctOn(q,expr.Group(buyer)).
func DistinctOn[S, R any](q Query[S, R], keys ...expr.GroupKey[S]) Query[S, R] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, key := range keys {
		inputs[i] = expr.FoundryGroup(key, sqlowner.Expressions())
	}
	q.source = ormcore.Distinct(q.source, querycore.DistinctOn, inputs)
	return q
}

// DistinctOnValues accepts selected keys while preserving grouping validation.
// Example: rows.DistinctOnValues(q,expr.Key(expr.AsSelected(count))).
func DistinctOnValues[S, R any](q Query[S, R], keys ...expr.ProjectionKey[S]) Query[S, R] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, key := range keys {
		inputs[i] = expr.FoundryKey(key, sqlowner.Expressions())
	}
	q.source = ormcore.Distinct(q.source, querycore.DistinctOn, inputs)
	return q
}
