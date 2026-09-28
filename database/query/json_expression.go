package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSONKind identifies a JSON value's top-level shape. JSONNull is distinct from SQL NULL.
type JSONKind string

const (
	JSONObject  JSONKind = "object"
	JSONArray   JSONKind = "array"
	JSONString  JSONKind = "string"
	JSONNumber  JSONKind = "number"
	JSONBoolean JSONKind = "boolean"
	JSONNull    JSONKind = "null"
)

func (k JSONKind) Validate() error {
	switch k {
	case JSONObject, JSONArray, JSONString, JSONNumber, JSONBoolean, JSONNull:
		return nil
	default:
		return fault.New(fault.Invalid, "invalid JSON kind")
	}
}

func jsonKindCodec() codec.Codec[JSONKind] {
	return codec.String[JSONKind]().Validated(JSONKind.Validate)
}

// JSONType returns a row value's JSON kind, retaining the input query owner.
func JSONType[S any, V jsonValue](input RowValue[S, V]) RowExpression[S, JSONKind] {
	return RowExpression[S, JSONKind]{JSONTypeValue(rowInput(input).Value())}
}
func JSONTypeValue[S any, V jsonValue](input Expression[S, V]) Expression[S, JSONKind] {
	return operationValue[S](jsonKindOperation, jsonKindCodec(), operationArg(input))
}

// JSONTypeNullable preserves SQL NULL; a present JSON null produces JSONNull.
func JSONTypeNullable[S any, V jsonValue](input RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[JSONKind]] {
	return RowExpression[S, value.Nullable[JSONKind]]{JSONTypeNullableValue(rowInput(input).Value())}
}
func JSONTypeNullableValue[S any, V jsonValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[JSONKind]] {
	return operationValue[S](jsonKindOperation, codec.Nullable(jsonKindCodec()), operationArg(input))
}

// JSONContains tests JSONB containment between the same concrete payload type.
func JSONContains[S any, V jsonValue](left, right RowValue[S, V]) RowExpression[S, bool] {
	return RowExpression[S, bool]{JSONContainsValue(rowInput(left).Value(), rowInput(right).Value())}
}
func JSONContainsValue[S any, V jsonValue](left, right Expression[S, V]) Expression[S, bool] {
	return operationValue[S](jsonContainsOperation, codec.Bool[bool](), operationArg(left), operationArg(right))
}

// JSONContainsNullable propagates SQL NULL. Widen non-null inputs with NullableRow.
func JSONContainsNullable[S any, V jsonValue](left, right RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[bool]] {
	return RowExpression[S, value.Nullable[bool]]{JSONContainsNullableValue(rowInput(left).Value(), rowInput(right).Value())}
}
func JSONContainsNullableValue[S any, V jsonValue](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[bool]] {
	return operationValue[S](jsonContainsOperation, codec.Nullable(codec.Bool[bool]()), operationArg(left), operationArg(right))
}

// JSONContainedBy reverses JSONContains while retaining both operand types.
func JSONContainedBy[S any, V jsonValue](left, right RowValue[S, V]) RowExpression[S, bool] {
	return RowExpression[S, bool]{JSONContainedByValue(rowInput(left).Value(), rowInput(right).Value())}
}
func JSONContainedByValue[S any, V jsonValue](left, right Expression[S, V]) Expression[S, bool] {
	return operationValue[S](jsonContainedByOperation, codec.Bool[bool](), operationArg(left), operationArg(right))
}

// JSONContainedByNullable retains a nullable result for nullable operands.
func JSONContainedByNullable[S any, V jsonValue](left, right RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[bool]] {
	return RowExpression[S, value.Nullable[bool]]{JSONContainedByNullableValue(rowInput(left).Value(), rowInput(right).Value())}
}
func JSONContainedByNullableValue[S any, V jsonValue](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[bool]] {
	return operationValue[S](jsonContainedByOperation, codec.Nullable(codec.Bool[bool]()), operationArg(left), operationArg(right))
}
