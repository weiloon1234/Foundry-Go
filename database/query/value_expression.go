package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type valueExpression = querycore.ValueExpression

type selectItem = querycore.SelectItem

func columnSelections(table string, columns []columnData) []selectItem {
	return querycore.ColumnSelections(table, columns)
}

// Expression is a SQL value with a compiler-checked input scope and result type.
// It retains its codec for typed composition; SQL names remain private metadata.
type Expression[S, V any] struct {
	_     [0]*S
	node  valueExpression
	codec codec.Codec[V]
}

func (a Aggregate[M, V]) Value() Expression[M, V] {
	return Expression[M, V]{node: a.node, codec: a.codec}
}

// Nullable explicitly widens a non-nullable expression's result type without
// changing its SQL value. It does not substitute a value for SQL NULL.
func Nullable[S, V any](e Expression[S, V]) Expression[S, value.Nullable[V]] {
	return Expression[S, value.Nullable[V]]{node: e.node, codec: codec.Nullable(e.codec)}
}

// Group is a model/scope-owned row key for GroupBy, DistinctOn and PartitionBy.
// Use a generated field's or computed RowExpression's Group method.
type Group[S any] struct {
	_    [0]*S
	node valueExpression
}

func (f ScalarField[M, V]) Group() Group[M] { return Group[M]{node: f.ref} }
