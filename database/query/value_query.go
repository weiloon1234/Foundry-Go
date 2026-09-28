package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ValueQuery retains input scope S while selecting exactly one value of type V.
// It shares projection validation, codecs and execution with declared records.
type ValueQuery[S, V any] struct {
	query ProjectionQuery[S, V]
	codec codec.Codec[V]
}

const valueColumnName = "value"

// SelectValue selects one typed expression for membership, scalar subqueries or
// direct streaming. To select a declared report's output, alias that report first.
func SelectValue[S, V any](source ProjectionSource[S], expression Expression[S, V]) ValueQuery[S, V] {
	field := NewProjectionField[V, V](valueColumnName)
	definition := DefineProjection([]ProjectionColumn[V]{field.Column()}, func(row database.Row) (V, error) {
		var result V
		err := row.Scan(expression.codec.Scan(&result))
		return result, err
	})
	return ValueQuery[S, V]{query: Project(source, definition, Map(field, expression)), codec: expression.codec}
}

func (q ValueQuery[S, V]) Where(predicates ...Predicate[S]) ValueQuery[S, V] {
	q.query = q.query.Where(predicates...)
	return q
}
func (q ValueQuery[S, V]) GroupBy(groups ...Group[S]) ValueQuery[S, V] {
	q.query = q.query.GroupBy(groups...)
	return q
}
func (q ValueQuery[S, V]) Having(predicates ...HavingPredicate[S]) ValueQuery[S, V] {
	q.query = q.query.Having(predicates...)
	return q
}
func (q ValueQuery[S, V]) OrderBy(orders ...ProjectionOrder[S]) ValueQuery[S, V] {
	q.query = q.query.OrderBy(orders...)
	return q
}
func (q ValueQuery[S, V]) Limit(count int) ValueQuery[S, V] {
	q.query = q.query.Limit(count)
	return q
}
func (q ValueQuery[S, V]) Offset(count int) ValueQuery[S, V] {
	q.query = q.query.Offset(count)
	return q
}
func (q ValueQuery[S, V]) Compile() (Statement, error) { return q.query.Compile() }
func (q ValueQuery[S, V]) Each(ctx context.Context, executor database.Executor, yield func(V) error) error {
	return q.query.Each(ctx, executor, yield)
}
func (q ValueQuery[S, V]) All(ctx context.Context, executor database.Executor) ([]V, error) {
	return q.query.All(ctx, executor)
}
func (q ValueQuery[S, V]) First(ctx context.Context, executor database.Executor) (value.Optional[V], error) {
	return q.query.First(ctx, executor)
}
func (q ValueQuery[S, V]) RequireFirst(ctx context.Context, executor database.Executor) (V, error) {
	return q.query.RequireFirst(ctx, executor)
}
func (q ValueQuery[S, V]) Count(ctx context.Context, executor database.Executor) (int64, error) {
	return q.query.Count(ctx, executor)
}
func (q ValueQuery[S, V]) Exists(ctx context.Context, executor database.Executor) (bool, error) {
	return q.query.Exists(ctx, executor)
}

// ValueQuerySource seals a single-column SELECT with a concrete result type.
// Its inner scope is intentionally independent of an outer predicate's scope.
type ValueQuerySource[V any] interface{ valueQuery() valueSubquery[V] }
type valueSubquery[V any] struct {
	subquery
	codec codec.Codec[V]
}

func (q ValueQuery[S, V]) subquery() subquery { return q.query.subquery() }
func (q ValueQuery[S, V]) valueQuery() valueSubquery[V] {
	return valueSubquery[V]{subquery: q.subquery(), codec: q.codec}
}
