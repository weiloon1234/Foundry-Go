package expr

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSONKind is the shared JSON shape identity; JSON null remains distinct from SQL NULL.
type JSONKind = database.JSONKind

const (
	JSONObject  = database.JSONObject
	JSONArray   = database.JSONArray
	JSONString  = database.JSONString
	JSONNumber  = database.JSONNumber
	JSONBoolean = database.JSONBoolean
	JSONNull    = database.JSONNull
)

func jsonKindCodec() codec.Codec[JSONKind] {
	return codec.String[JSONKind]().Validated(JSONKind.Validate)
}
func jsonKindColumn() ormcore.Column[JSONKind] { return columnCodec(jsonKindCodec()) }
func nullableJSONKindColumn() ormcore.Column[value.Nullable[JSONKind]] {
	return columnCodec(codec.Nullable(jsonKindCodec()))
}
func jsonLengthColumn() ormcore.Column[value.Nullable[int64]] {
	return columnCodec(codec.Nullable(codec.Signed[int64]()))
}
func jsonBoolColumn() ormcore.Column[bool] { return columnCodec(codec.Bool[bool]()) }
func nullableJSONBoolColumn() ormcore.Column[value.Nullable[bool]] {
	return columnCodec(codec.Nullable(codec.Bool[bool]()))
}

// JSONType evaluates JSON shape or safe array length in the row phase.
// Example: expr.JSONType(Value(Documents.Settings)).
func JSONType[S, P any](input RowValue[S, value.JSON[P]]) RowExpression[S, JSONKind] {
	return RowExpression[S, JSONKind]{data: JSONTypeValue(rowInput(input).Value()).data}
}

// JSONTypeValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONTypeValue(Select(Value(Documents.Settings))).
func JSONTypeValue[S, P any](input Selected[S, value.JSON[P]]) Selected[S, JSONKind] {
	return operationValue[S](querycore.JsonKindOperation, jsonKindColumn(), operationArg(input))
}

// JSONTypeNullable evaluates JSON shape or safe array length in the row phase; SQL NULL propagates.
// Example: expr.JSONTypeNullable(Value(Documents.Backup)).
func JSONTypeNullable[S, P any](input RowValue[S, value.Nullable[value.JSON[P]]]) RowExpression[S, value.Nullable[JSONKind]] {
	return RowExpression[S, value.Nullable[JSONKind]]{data: JSONTypeNullableValue(rowInput(input).Value()).data}
}

// JSONTypeNullableValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONTypeNullableValue(Select(Value(Documents.Backup))).
func JSONTypeNullableValue[S, P any](input Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[JSONKind]] {
	return operationValue[S](querycore.JsonKindOperation, nullableJSONKindColumn(), operationArg(input))
}

// JSONContains evaluates JSON containment between identical payloads in the row phase.
// Example: expr.JSONContains(Value(Documents.Settings), Value(Documents.Settings)).
func JSONContains[S, P any](left, right RowValue[S, value.JSON[P]]) RowExpression[S, bool] {
	return RowExpression[S, bool]{data: JSONContainsValue(rowInput(left).Value(), rowInput(right).Value()).data}
}

// JSONContainsValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONContainsValue(Select(Value(Documents.Settings)), Select(Value(Documents.Settings))).
func JSONContainsValue[S, P any](left, right Selected[S, value.JSON[P]]) Selected[S, bool] {
	return operationValue[S](querycore.JsonContainsOperation, jsonBoolColumn(), operationArg(left), operationArg(right))
}

// JSONContainsNullable evaluates JSON containment between identical payloads in the row phase; SQL NULL propagates.
// Example: expr.JSONContainsNullable(Value(Documents.Backup), Value(Documents.Backup)).
func JSONContainsNullable[S, P any](left, right RowValue[S, value.Nullable[value.JSON[P]]]) RowExpression[S, value.Nullable[bool]] {
	return RowExpression[S, value.Nullable[bool]]{data: JSONContainsNullableValue(rowInput(left).Value(), rowInput(right).Value()).data}
}

// JSONContainsNullableValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONContainsNullableValue(Select(Value(Documents.Backup)), Select(Value(Documents.Backup))).
func JSONContainsNullableValue[S, P any](left, right Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[bool]] {
	return operationValue[S](querycore.JsonContainsOperation, nullableJSONBoolColumn(), operationArg(left), operationArg(right))
}

// JSONContainedBy evaluates JSON containment between identical payloads in the row phase.
// Example: expr.JSONContainedBy(Value(Documents.Settings), Value(Documents.Settings)).
func JSONContainedBy[S, P any](left, right RowValue[S, value.JSON[P]]) RowExpression[S, bool] {
	return RowExpression[S, bool]{data: JSONContainedByValue(rowInput(left).Value(), rowInput(right).Value()).data}
}

// JSONContainedByValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONContainedByValue(Select(Value(Documents.Settings)), Select(Value(Documents.Settings))).
func JSONContainedByValue[S, P any](left, right Selected[S, value.JSON[P]]) Selected[S, bool] {
	return operationValue[S](querycore.JsonContainedByOperation, jsonBoolColumn(), operationArg(left), operationArg(right))
}

// JSONContainedByNullable evaluates JSON containment between identical payloads in the row phase; SQL NULL propagates.
// Example: expr.JSONContainedByNullable(Value(Documents.Backup), Value(Documents.Backup)).
func JSONContainedByNullable[S, P any](left, right RowValue[S, value.Nullable[value.JSON[P]]]) RowExpression[S, value.Nullable[bool]] {
	return RowExpression[S, value.Nullable[bool]]{data: JSONContainedByNullableValue(rowInput(left).Value(), rowInput(right).Value()).data}
}

// JSONContainedByNullableValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONContainedByNullableValue(Select(Value(Documents.Backup)), Select(Value(Documents.Backup))).
func JSONContainedByNullableValue[S, P any](left, right Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[bool]] {
	return operationValue[S](querycore.JsonContainedByOperation, nullableJSONBoolColumn(), operationArg(left), operationArg(right))
}

// JSONLength evaluates JSON shape or safe array length in the row phase.
// Example: expr.JSONLength(Value(Documents.Settings)).
func JSONLength[S, P any](input RowValue[S, value.JSON[P]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(JSONLengthValue(rowInput(input).Value()))
}

// JSONLengthValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONLengthValue(Select(Value(Documents.Settings))).
func JSONLengthValue[S, P any](input Selected[S, value.JSON[P]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.JsonArrayLengthOperation, jsonLengthColumn(), operationArg(input))
}

// JSONLengthNullable evaluates JSON shape or safe array length in the row phase; SQL NULL propagates.
// Example: expr.JSONLengthNullable(Value(Documents.Backup)).
func JSONLengthNullable[S, P any](input RowValue[S, value.Nullable[value.JSON[P]]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(JSONLengthNullableValue(rowInput(input).Value()))
}

// JSONLengthNullableValue preserves selected phase, including aggregates and windows.
// Example: expr.JSONLengthNullableValue(Select(Value(Documents.Backup))).
func JSONLengthNullableValue[S, P any](input Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.JsonArrayLengthOperation, jsonLengthColumn(), operationArg(input))
}
