// Package expr provides thin, typed SQL expressions in explicit source scopes.
// Example: expr.At(rows.Scope(members), orm.TextExpr(Members.Name)).
package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Scope keeps a model M inside source S.
//
// Example: rows.Scope(members)
type Scope[S, M any] struct {
	_    [0]*S
	_    [0]*M
	core ormcore.Scope
}

// Expression keeps a scoped SQL value and its codec closures.
//
// Example: expr.At(scope, orm.KeyExpr(Orders.ID))
type Expression[S, V, P any] = expression[*S, V, P]

type expression[S, V, P any] struct {
	_          [0]S
	_          [0]*P
	core       ormcore.Expression
	column     ormcore.Column[V]
	projection func() ormcore.Column[P]
}

// Predicate keeps source ownership on a SQL condition.
//
// Example: expr.Eq(tenant, 7)
type Predicate[S any] = predicate[*S]

type predicate[S any] struct {
	predicatePhase [0]struct{}
	_              [0]S
	core           ormcore.Predicate
}

// Order keeps source ownership on ordering.
//
// Example: expr.Asc(id)
type Order[S any] struct {
	_    [0]*S
	core ormcore.Order
}

// At re-scopes a model column without erasing its value type.
//
// Example: expr.At(scope, column)
func At[S, M, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) Expression[S, V, P] {
	c, projected := orm.FoundryColumn(column, sqlowner.Metadata())
	err := safeerror.Join("operation failed", c.Err, scope.core.Err)
	if scope.core.Owner == nil || scope.core.Owner != c.Owner || scope.core.Table == "" {
		err = safeerror.Join("operation failed", err, ormcore.Invalid("column belongs to a different model scope"))
	}
	c.Field.Table = scope.core.Table
	c.Reference = scope.core.Reference
	projected.Reference = scope.core.Reference
	return Expression[S, V, P]{column: c, projection: func() ormcore.Column[P] { return projected }, core: ormcore.Expression{Node: c.Field, Refs: []ormcore.Reference{c.Reference}, Err: err}}
}

// Value uses a column in its unaliased model scope.
//
// Example: expr.Value(column)
func Value[M, V, P any](column orm.ExpressionColumn[M, V, P]) Expression[M, V, P] {
	c, projected := orm.FoundryColumn(column, sqlowner.Metadata())
	return Expression[M, V, P]{column: c, projection: func() ormcore.Column[P] { return projected }, core: ormcore.Expression{Node: c.Field, Refs: []ormcore.Reference{c.Reference}, Err: c.Err}}
}

// Eq compares a row value.
//
// Example: expr.Eq(e, v)
func Eq[S, V any](e RowValue[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.Equal, v)}
}

// Ne excludes a row value.
//
// Example: expr.Ne(e, v)
func Ne[S, V any](e RowValue[S, V], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.NotEqual, v)}
}

// In tests membership.
//
// Example: expr.In(e, vs...)
func In[S, V any](e RowValue[S, V], vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.In, vs...)}
}

// NullableEq compares a nullable row value with a present value.
//
// Example: expr.NullableEq(e, v)
func NullableEq[S, V any](e RowValue[S, value.Nullable[V]], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.Equal, presentValues([]V{v})...)}
}

// NullableNe excludes a present value from a nullable row value.
//
// Example: expr.NullableNe(e, v)
func NullableNe[S, V any](e RowValue[S, value.Nullable[V]], v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.NotEqual, presentValues([]V{v})...)}
}

// NullableIn tests membership of a present value in a nullable row value.
//
// Example: expr.NullableIn(e, vs...)
func NullableIn[S, V any](e RowValue[S, value.Nullable[V]], vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.In, presentValues(vs)...)}
}

// Asc orders a scoped value ascending.
//
// Example: expr.Asc(e)
func Asc[S, V any](e RowValue[S, V]) Order[S] {
	return Order[S]{core: ormcore.OrderExpression(rowInput(e).data.core, false)}
}

// Desc orders a scoped value descending.
//
// Example: expr.Desc(e)
func Desc[S, V any](e RowValue[S, V]) Order[S] {
	return Order[S]{core: ormcore.OrderExpression(rowInput(e).data.core, true)}
}

// NullsFirst explicitly sorts SQL NULL before values.
//
// Example: expr.Asc(name).NullsFirst()
func (o Order[S]) NullsFirst() Order[S] { o.core.Node.Nulls = querycore.NullsFirst; return o }

// NullsLast explicitly sorts SQL NULL after values.
//
// Example: expr.Asc(name).NullsLast()
func (o Order[S]) NullsLast() Order[S] { o.core.Node.Nulls = querycore.NullsLast; return o }

// FoundryScope constructs a sealed scope for rows.
//
// Example: expr.FoundryScope[User, User](core, seal)
func FoundryScope[S, M any](core ormcore.Scope, seal sqlowner.ExpressionSeal) Scope[S, M] {
	if !seal.Valid() {
		return Scope[S, M]{core: ormcore.Scope{Err: ormcore.Invalid("invalid SQL owner capability")}}
	}

	return Scope[S, M]{core: core}
}

// FoundryScopeData exposes sealed metadata to rows.
//
// Example: expr.FoundryScopeData(s, seal)
func FoundryScopeData[S, M any](s Scope[S, M], seal sqlowner.ExpressionSeal) ormcore.Scope {
	if !seal.Valid() {
		return ormcore.Scope{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return s.core
}

// FoundryExpression exposes sealed SQL metadata to rows.
//
// Example: expr.FoundryExpression(e, seal)
func FoundryExpression[S, V, P any](e Expression[S, V, P], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}

	if e.projection != nil {
		e.core.Sensitive = e.core.Sensitive || e.projection().Sensitive
	}
	return e.core
}

// FoundryPredicate exposes a sealed condition to rows.
//
// Example: expr.FoundryPredicate(p, seal)
func FoundryPredicate[S any](p Predicate[S], seal sqlowner.ExpressionSeal) ormcore.Predicate {
	if !seal.Valid() {
		return ormcore.Predicate{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return p.core
}

// FoundryOrder exposes sealed ordering to rows.
//
// Example: expr.FoundryOrder(o, seal)
func FoundryOrder[S any](o Order[S], seal sqlowner.ExpressionSeal) ormcore.Order {
	if !seal.Valid() {
		return ormcore.Order{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return o.core
}

// IsNull tests SQL nullness on a nullable expression.
//
// Example: expr.IsNull(e)
func IsNull[S, V any](e RowValue[S, V]) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.IsNull)}
}

// IsNotNull tests SQL presence on a nullable expression.
//
// Example: expr.IsNotNull(e)
func IsNotNull[S, V any](e RowValue[S, V]) Predicate[S] {
	return Predicate[S]{core: rowCompare(rowInput(e).data, querycore.IsNotNull)}
}

// And combines conditions within one source scope.
//
// Example: expr.And(ps...)
func And[S any](ps ...Predicate[S]) Predicate[S] {
	if len(ps) > querycore.MaxExpressionNodes {
		return Predicate[S]{core: ormcore.Predicate{Err: ormcore.Invalid("conjunction exceeds its resource bound")}}
	}
	inputs := make([]ormcore.Predicate, len(ps))
	for i, p := range ps {
		inputs[i] = p.core
	}
	return Predicate[S]{core: ormcore.Conjunction(inputs)}
}
