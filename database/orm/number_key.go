package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Numeric key adapters retain reads and ordinary identity assignment without adjustment methods.
type numberKeyColumn[M any, V NumberValue, R keyRole] struct {
	_      [0]*M
	role   [0]R
	number columnData[V]
}

func (numberKeyColumn[M, V, R]) orderedColumn() {}

// Eq compares a concrete value. Example: Orders.ID.Eq(100).
func (c numberKeyColumn[M, V, R]) Eq(v V) Predicate[M] {
	return comparison[M](c.number, querycore.Equal, []V{v})
}

// Ne excludes a concrete value. Example: Orders.ID.Ne(100).
func (c numberKeyColumn[M, V, R]) Ne(v V) Predicate[M] {
	return comparison[M](c.number, querycore.NotEqual, []V{v})
}

// In tests membership. Example: Orders.ID.In(a, b).
func (c numberKeyColumn[M, V, R]) In(v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.In, v)
}

// NotIn excludes membership. Example: Orders.ID.NotIn(a, b).
func (c numberKeyColumn[M, V, R]) NotIn(v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Orders.ID.Asc().
func (c numberKeyColumn[M, V, R]) Asc() Order[M] { return columnOrder[M](c.number, false) }

// Desc sorts descending. Example: Orders.ID.Desc().
func (c numberKeyColumn[M, V, R]) Desc() Order[M] { return columnOrder[M](c.number, true) }

// Set assigns a value, including zero. Example: Orders.ID.Set(100).
func (c numberKeyColumn[M, V, R]) Set(v V) Assignment[M] { return columnAssignment[M](c.number, v) }

// Gt compares ordered values. Example: Orders.ID.Gt(100).
func (c numberKeyColumn[M, V, R]) Gt(v V) Predicate[M] {
	return comparison[M](c.number, querycore.Greater, []V{v})
}

// Gte compares ordered values. Example: Orders.ID.Gte(100).
func (c numberKeyColumn[M, V, R]) Gte(v V) Predicate[M] {
	return comparison[M](c.number, querycore.GreaterOrEqual, []V{v})
}

// Lt compares ordered values. Example: Orders.ID.Lt(100).
func (c numberKeyColumn[M, V, R]) Lt(v V) Predicate[M] {
	return comparison[M](c.number, querycore.Less, []V{v})
}

// Lte compares ordered values. Example: Orders.ID.Lte(100).
func (c numberKeyColumn[M, V, R]) Lte(v V) Predicate[M] {
	return comparison[M](c.number, querycore.LessOrEqual, []V{v})
}

// Between includes both endpoints. Example: Orders.ID.Between(a, b).
func (c numberKeyColumn[M, V, R]) Between(a, b V) Predicate[M] { return between[M](c.number, a, b) }

func (c numberKeyColumn[M, V, R]) ormColumn(*M) columnData[V] { return c.number }
func (c numberKeyColumn[M, V, R]) expressionColumn(*M) (ormcore.Column[V], ormcore.Column[V]) {
	return requiredExpression(c.number)
}
