package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Length is the element count of a JSON array at this path. It is SQL NULL
// when the path is missing or holds any other JSON kind, so it never fails on
// a scalar or object.
func (p JSONPath[S, P]) Length() NullableOrderedRowExpression[S, int64] {
	return OrderNullableRow(RowExpression[S, value.Nullable[int64]]{operationValue[S](jsonArrayLengthOperation, codec.Nullable(codec.Signed[int64]()), operationArg(p.expression))})
}

// Contains uses PostgreSQL JSONB containment at this path: every key and
// element of the typed fragment must be present. A missing path never matches.
func (p JSONPath[S, P]) Contains(fragment P) Predicate[S] {
	contains := RowExpression[S, value.Nullable[bool]]{JSONContainsNullableValue(p.expression, jsonFragment[S](fragment))}
	return contains.Eq(value.Of(true))
}

// jsonFragment binds one typed payload as a JSONB parameter.
func jsonFragment[S, P any](fragment P) Expression[S, value.Nullable[value.JSON[P]]] {
	c := codec.Nullable(codec.JSON[P]())
	document, err := value.NewJSON(fragment)
	if err != nil {
		return Expression[S, value.Nullable[value.JSON[P]]]{node: parameterNode{err: fault.New(fault.Invalid, "JSON fragment cannot be encoded")}, codec: c}
	}
	return parameterExpression[S](value.Of(document), c).Value()
}

// Length is the element count of an array document; SQL NULL for other kinds.
func (f JSONField[M, V]) Length() NullableOrderedRowExpression[M, int64] {
	return OrderNullableRow(RowExpression[M, value.Nullable[int64]]{operationValue[M](jsonArrayLengthOperation, codec.Nullable(codec.Signed[int64]()), operationArg(rowInput(f).Value()))})
}

// Length is the element count of an array document; SQL NULL for other kinds
// and for a NULL column.
func (f NullableJSONField[M, V]) Length() NullableOrderedRowExpression[M, int64] {
	return OrderNullableRow(RowExpression[M, value.Nullable[int64]]{operationValue[M](jsonArrayLengthOperation, codec.Nullable(codec.Signed[int64]()), operationArg(rowInput(f).Value()))})
}
