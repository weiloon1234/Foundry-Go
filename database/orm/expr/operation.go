package expr

import (
	"database/sql/driver"
	"errors"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type exactNumber interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | decimal.Decimal
}
type numericValue interface{ orm.NumberValue }
type orderedScalar interface {
	numericValue | ~string | time.Time | temporal.Date | temporal.Time | temporal.DateTime | temporal.LocalDateTime | temporal.Interval
}

func operationArg[S, V any](e Selected[S, V]) ormcore.OperationArgument {
	return ormcore.OperationArgument{Value: e.data.core, Kind: e.data.column.Kind}
}
func operationValue[S, V any](op querycore.ScalarOperation, c ormcore.Column[V], args ...ormcore.OperationArgument) Selected[S, V] {
	core := ormcore.Operation(op, c.Kind, args...)
	core.Err = errors.Join(core.Err, c.Err)
	return Selected[S, V]{data: rowData[V]{core: core, column: c}}
}
func rowCompare[V any](d rowData[V], op querycore.Operator, values ...V) ormcore.Predicate {
	err := d.core.Err
	if d.core.Node == nil || d.column.Bind == nil || d.column.Clone == nil {
		return ormcore.Predicate{Err: errors.Join(err, ormcore.Invalid("uninitialized expression"))}
	}
	captured := make([]any, len(values))
	for i, v := range values {
		captured[i] = d.column.Clone(v)
	}
	return ormcore.Predicate{Node: querycore.Comparison{Operand: d.core.Node, Operator: op, Values: captured, Kind: d.column.Kind, Bind: func(v any) (driver.Value, error) { return d.column.Bind(v.(V)) }}, Refs: d.core.Refs, ReferenceFree: d.core.ReferenceFree, Err: err}
}
func rowOrder[S, V any](d rowData[V], descending bool) Order[S] {
	return Order[S]{core: ormcore.Order{Node: querycore.OrderNode{Expression: d.core.Node, Descending: descending}, Refs: d.core.Refs, ReferenceFree: d.core.ReferenceFree, Err: d.core.Err}}
}
func presentValues[V any](vs []V) []value.Nullable[V] {
	out := make([]value.Nullable[V], len(vs))
	for i, v := range vs {
		out[i] = value.Of(v)
	}
	return out
}
