// Package expr provides thin, typed SQL expressions in explicit source scopes.
// Example: expr.At(rows.Scope(members), Members.Name).
package expr

import (
	"database/sql/driver"
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Scope keeps a model M inside source S. Example: rows.Scope(members).
type Scope[S, M any] struct {
	_    [0]*S
	_    [0]*M
	core ormcore.Scope
}

// Expression keeps a scoped SQL value and its codec closures. Example: expr.At(scope,Orders.ID).
type Expression[S, V, P any] struct {
	_      [0]*S
	_      [0]*P
	core   ormcore.Expression
	column ormcore.Column[V]
}

// Predicate keeps source ownership on a SQL condition. Example: tenant.Eq(7).
type Predicate[S any] struct {
	_    [0]*S
	core ormcore.Predicate
}

// Order keeps source ownership on ordering. Example: id.Asc().
type Order[S any] struct {
	_    [0]*S
	core ormcore.Order
}

// At re-scopes a model column without erasing its value type. Example: expr.At(rows.Scope(members),Members.Name).
func At[S, M, V, P any](scope Scope[S, M], column orm.ExpressionColumn[M, V, P]) Expression[S, V, P] {
	c, _ := orm.FoundryColumn(column, sqlowner.Seal{})
	err := errors.Join(c.Err, scope.core.Err)
	if scope.core.Owner == nil || scope.core.Owner != c.Owner || scope.core.Table == "" {
		err = errors.Join(err, ormcore.Invalid("column belongs to a different model scope"))
	}
	c.Field.Table = scope.core.Table
	c.Reference = scope.core.Reference
	return Expression[S, V, P]{column: c, core: ormcore.Expression{Node: c.Field, Refs: []ormcore.Reference{c.Reference}, Err: err}}
}

// Value uses a column in its unaliased model scope. Example: expr.Value(Orders.TotalCents).
func Value[M, V, P any](column orm.ExpressionColumn[M, V, P]) Expression[M, V, P] {
	c, _ := orm.FoundryColumn(column, sqlowner.Seal{})
	return Expression[M, V, P]{column: c, core: ormcore.Expression{Node: c.Field, Refs: []ormcore.Reference{c.Reference}, Err: c.Err}}
}

// Eq compares a scoped value. Example: memberTenant.Eq(7).
func (e Expression[S, V, P]) Eq(v V) Predicate[S] { return compare(e, querycore.Equal, v) }

// Ne excludes a scoped value. Example: memberName.Ne("").
func (e Expression[S, V, P]) Ne(v V) Predicate[S] { return compare(e, querycore.NotEqual, v) }
func compare[S, V, P any](e Expression[S, V, P], op querycore.Operator, v V) Predicate[S] {
	err := e.core.Err
	if e.column.Bind == nil || e.column.Clone == nil {
		return Predicate[S]{core: ormcore.Predicate{Err: ormcore.Invalid("uninitialized expression")}}
	}
	return Predicate[S]{core: ormcore.Predicate{Refs: e.core.Refs, Err: err, Node: querycore.Comparison{Operand: e.core.Node, Operator: op, Values: []any{e.column.Clone(v)}, Kind: e.column.Kind, Bind: func(v any) (driver.Value, error) { return e.column.Bind(v.(V)) }}}}
}

// Asc orders a scoped value ascending. Example: orderID.Asc().
func (e Expression[S, V, P]) Asc() Order[S] { return order(e, false) }

// Desc orders a scoped value descending. Example: orderID.Desc().
func (e Expression[S, V, P]) Desc() Order[S] { return order(e, true) }
func order[S, V, P any](e Expression[S, V, P], descending bool) Order[S] {
	return Order[S]{core: ormcore.Order{Node: querycore.OrderNode{Expression: e.core.Node, Descending: descending}, Refs: e.core.Refs, Err: e.core.Err}}
}

// NullsFirst explicitly sorts SQL NULL before values. Example: name.Asc().NullsFirst().
func (o Order[S]) NullsFirst() Order[S] { o.core.Node.Nulls = querycore.NullsFirst; return o }

// NullsLast explicitly sorts SQL NULL after values. Example: name.Asc().NullsLast().
func (o Order[S]) NullsLast() Order[S] { o.core.Node.Nulls = querycore.NullsLast; return o }

// FoundryScope constructs a sealed scope for rows. Example: FoundryScope[S,M](metadata,sqlowner.Seal{}).
func FoundryScope[S, M any](core ormcore.Scope, _ sqlowner.Seal) Scope[S, M] {
	return Scope[S, M]{core: core}
}

// FoundryScopeData exposes sealed metadata to rows. Example: FoundryScopeData(scope,sqlowner.Seal{}).
func FoundryScopeData[S, M any](s Scope[S, M], _ sqlowner.Seal) ormcore.Scope { return s.core }

// FoundryExpression exposes sealed SQL metadata to rows. Example: FoundryExpression(e,sqlowner.Seal{}).
func FoundryExpression[S, V, P any](e Expression[S, V, P], _ sqlowner.Seal) ormcore.Expression {
	return e.core
}

// FoundryPredicate exposes a sealed condition to rows. Example: FoundryPredicate(p,sqlowner.Seal{}).
func FoundryPredicate[S any](p Predicate[S], _ sqlowner.Seal) ormcore.Predicate { return p.core }

// FoundryOrder exposes sealed ordering to rows. Example: FoundryOrder(o,sqlowner.Seal{}).
func FoundryOrder[S any](o Order[S], _ sqlowner.Seal) ormcore.Order { return o.core }

// IsNull tests SQL nullness on a nullable expression.
func (e Expression[S, V, P]) IsNull() Predicate[S] { return nullComparison(e, querycore.IsNull) }

// IsNotNull tests SQL presence on a nullable expression.
func (e Expression[S, V, P]) IsNotNull() Predicate[S] { return nullComparison(e, querycore.IsNotNull) }
func nullComparison[S, V, P any](e Expression[S, V, P], op querycore.Operator) Predicate[S] {
	err := e.core.Err
	if !e.column.Nullable {
		err = errors.Join(err, ormcore.Invalid("nullness requires a nullable expression"))
	}
	return Predicate[S]{core: ormcore.Predicate{Node: querycore.Comparison{Operand: e.core.Node, Operator: op}, Refs: e.core.Refs, Err: err}}
}

// And combines conditions within one source scope. Example: expr.And(a.Eq(1),b.Eq(2)).
func And[S any](ps ...Predicate[S]) Predicate[S] {
	p := ormcore.Predicate{}
	children := make([]querycore.Expression, len(ps))
	for i, v := range ps {
		children[i] = v.core.Node
		p.Err = errors.Join(p.Err, v.core.Err)
		p.Refs = append(p.Refs, v.core.Refs...)
	}
	p.Refs = slices.Clip(p.Refs)
	p.Node = querycore.Junction{Children: children}
	return Predicate[S]{core: p}
}
