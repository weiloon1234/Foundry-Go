package orm

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Numeric key adapters retain reads and ordinary identity assignment without adjustment methods.
type numberKeyColumn[M any, V NumberValue, R keyRole] struct {
	_      [0]*M
	role   [0]R
	number columnData[V]
}

// NumberKeyEq compares a concrete value. Example: NumberKeyEq(Orders.ID, 100).
func NumberKeyEq[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.Equal, []V{v})
}

// NumberKeyNe excludes a concrete value. Example: NumberKeyNe(Orders.ID, 100).
func NumberKeyNe[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.NotEqual, []V{v})
}

// NumberKeyIn tests membership. Example: NumberKeyIn(Orders.ID, a, b).
func NumberKeyIn[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.In, v)
}

// NumberKeyNotIn excludes membership. Example: NumberKeyNotIn(Orders.ID, a, b).
func NumberKeyNotIn[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v ...V) Predicate[M] {
	return comparison[M](c.number, querycore.NotIn, v)
}

// NumberKeyAsc sorts ascending. Example: NumberKeyAsc(Orders.ID).
func NumberKeyAsc[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.number, false)
}

// NumberKeyDesc sorts descending. Example: NumberKeyDesc(Orders.ID).
func NumberKeyDesc[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R]) Order[M] {
	return columnOrder[M](c.number, true)
}

// NumberKeySet assigns a value, including zero. Example: NumberKeySet(Orders.ID, 100).
func NumberKeySet[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Assignment[M] {
	return columnAssignment[M](c.number, v)
}

// NumberKeyGt compares ordered values. Example: NumberKeyGt(Orders.ID, 100).
func NumberKeyGt[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.Greater, []V{v})
}

// NumberKeyGte compares ordered values. Example: NumberKeyGte(Orders.ID, 100).
func NumberKeyGte[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.GreaterOrEqual, []V{v})
}

// NumberKeyLt compares ordered values. Example: NumberKeyLt(Orders.ID, 100).
func NumberKeyLt[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.Less, []V{v})
}

// NumberKeyLte compares ordered values. Example: NumberKeyLte(Orders.ID, 100).
func NumberKeyLte[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], v V) Predicate[M] {
	return comparison[M](c.number, querycore.LessOrEqual, []V{v})
}

// NumberKeyBetween includes both endpoints. Example: NumberKeyBetween(Orders.ID, a, b).
func NumberKeyBetween[M any, V NumberValue, R keyRole](c numberKeyColumn[M, V, R], a, b V) Predicate[M] {
	return between[M](c.number, a, b)
}
