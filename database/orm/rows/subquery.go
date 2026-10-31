package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SelectionSource retains a scope for projections and subqueries, without granting join access.
// Example: rows.SelectScalar(rows.From(query), expr.AsSelected(expr.Row(age))).
type SelectionSource[S any] interface{ rowSource(*S) ormcore.Source }

// Input retains a SELECT's scope without adding methods to model or row queries. Example: rows.From(Users.Query()).
type Input[S any] struct {
	_      [0]*S
	source ormcore.Source
}

func (s Input[S]) rowSource(*S) ormcore.Source { return s.source }

// Scalar retains exactly one selected value without standalone execution. Example: rows.SelectScalar(rows.From(q),expr.AsSelected(expr.Row(age))).
type Scalar[S, V any] struct {
	_      [0]*S
	_      [0]*V
	source ormcore.Source
	column ormcore.Column[V]
}

func (s Scalar[S, V]) rowSource(*S) ormcore.Source { return s.source }

// From adapts an ordinary model query for scoped SELECT composition. Example: rows.From(Users.Query().Limit(1)).
func From[M any](q orm.Query[M]) Input[M] {
	source := orm.FoundrySource(q, sqlowner.Metadata())
	// This is an unselected input; the consuming projection or subquery chooses
	// its output. FromRows retains an explicitly selected result instead.
	source.Node.Selections = nil
	return Input[M]{source: source}
}

// FromRows retains projected filters, windows and deferred schema checks. Example: rows.FromRows(report.Where(condition)).
func FromRows[S, R any](q Query[S, R]) Input[S] { return Input[S]{source: q.source} }

func sourceOf[S any](s SelectionSource[S]) ormcore.Source {
	if ormcore.NilDescriptor(s) {
		return ormcore.Source{Err: ormcore.Invalid("subquery requires a source")}
	}
	return s.rowSource(nil)
}

// SelectScalar selects one exact value for scalar, IN or EXISTS composition. Example: rows.SelectScalar(rows.From(q),expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(Users.Age))))).
func SelectScalar[S, V any](source SelectionSource[S], selected expr.Selected[S, V]) Scalar[S, V] {
	c := expr.FoundrySelectedColumn(selected, sqlowner.Expressions())
	s := ormcore.SelectScalar(sourceOf(source), expr.FoundrySelected(selected, sqlowner.Expressions()))
	s.Err = safeerror.Join("operation failed", s.Err, c.Err)
	if c.Bind == nil || c.Clone == nil {
		s.Err = safeerror.Join("operation failed", s.Err, ormcore.Invalid("scalar selection requires a value codec"))
	}
	return Scalar[S, V]{source: s, column: c}
}

// WhereScalar appends row predicates without changing the selected type. Example: rows.WhereScalar(inner,expr.Eq(age, 18)).
func WhereScalar[S, V any](q Scalar[S, V], predicates ...expr.Predicate[S]) Scalar[S, V] {
	q.source = whereSource(q.source, predicates)
	return q
}

// ExistsQuery anchors an independent SELECT's existence in the outer scope. Example: rows.ExistsQuery(rows.From(users),rows.From(orders)).
func ExistsQuery[S, I any](outer SelectionSource[S], inner SelectionSource[I]) expr.Predicate[S] {
	return expr.FoundryPredicateValue[S](ormcore.ExistsPredicate(sourceOf(outer), sourceOf(inner)), sqlowner.Expressions())
}

// InQuery requires an exact row/result type, including nullability. Example: rows.InQuery(expr.Value(orm.KeyExpr(Users.ID)),inner).
func InQuery[S, I, V any](left expr.RowValue[S, V], inner Scalar[I, V]) expr.Predicate[S] {
	return expr.FoundryPredicateValue[S](ormcore.MembershipPredicate(expr.FoundryRowValue(left, sqlowner.Expressions()), inner.source), sqlowner.Expressions())
}

// InNullableQuery explicitly admits nullable inner results without replacing NULLs. Example: rows.InNullableQuery(expr.Value(orm.KeyExpr(Users.ID)),nullableIDs).
func InNullableQuery[S, I, V any](left expr.RowValue[S, V], inner Scalar[I, value.Nullable[V]]) expr.Predicate[S] {
	return expr.FoundryPredicateValue[S](ormcore.MembershipPredicate(expr.FoundryRowValue(left, sqlowner.Expressions()), inner.source), sqlowner.Expressions())
}

// ScalarRowQuery evaluates an independent scalar in row comparisons or arithmetic. Example: expr.GreaterNullable(age,rows.ScalarRowQuery(outer,inner)).
func ScalarRowQuery[S, I, V any](outer SelectionSource[S], inner Scalar[I, V]) expr.RowExpression[S, value.Nullable[V]] {
	core := ormcore.ScalarExpression(sourceOf(outer), inner.source)
	v := expr.Nullable(expr.FoundrySelectedValue[S](core, inner.column, sqlowner.Expressions()))
	return expr.FoundryRowExpression[S](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

// ScalarNullableRowQuery retains one nullable layer in row expressions. Example: rows.ScalarNullableRowQuery(outer,nullableInner).
func ScalarNullableRowQuery[S, I, V any](outer SelectionSource[S], inner Scalar[I, value.Nullable[V]]) expr.RowExpression[S, value.Nullable[V]] {
	v := expr.FoundrySelectedValue[S](ormcore.ScalarExpression(sourceOf(outer), inner.source), inner.column, sqlowner.Expressions())
	return expr.FoundryRowExpression[S](expr.FoundrySelected(v, sqlowner.Expressions()), expr.FoundrySelectedColumn(v, sqlowner.Expressions()), sqlowner.Expressions())
}

func whereSource[S any](source ormcore.Source, predicates []expr.Predicate[S]) ormcore.Source {
	inputs := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		inputs[i] = expr.FoundryPredicate(p, sqlowner.Expressions())
	}
	return ormcore.WhereSource(source, inputs)
}
