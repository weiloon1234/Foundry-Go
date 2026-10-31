package expr

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func jsonKindCodec() codec.Codec[database.JSONKind] {
	return codec.String[database.JSONKind]().Validated(database.JSONKind.Validate)
}
func jsonKindColumn() ormcore.Column[database.JSONKind] { return columnCodec(jsonKindCodec()) }
func nullableJSONKindColumn() ormcore.Column[value.Nullable[database.JSONKind]] {
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
//
// Example: expr.JSONType(input)
func JSONType[S, P any](input RowValue[S, value.JSON[P]]) RowExpression[S, database.JSONKind] {
	return RowExpression[S, database.JSONKind]{data: JSONTypeValue(AsSelected(rowInput(input))).data}
}

// JSONTypeValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONTypeValue(input)
func JSONTypeValue[S, P any](input Selected[S, value.JSON[P]]) Selected[S, database.JSONKind] {
	return operationValue[S](querycore.JsonKindOperation, jsonKindColumn(), operationArg(input))
}

// JSONTypeNullable evaluates JSON shape or safe array length in the row phase; SQL NULL propagates.
//
// Example: expr.JSONTypeNullable(input)
func JSONTypeNullable[S, P any](input RowValue[S, value.Nullable[value.JSON[P]]]) RowExpression[S, value.Nullable[database.JSONKind]] {
	return RowExpression[S, value.Nullable[database.JSONKind]]{data: JSONTypeNullableValue(AsSelected(rowInput(input))).data}
}

// JSONTypeNullableValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONTypeNullableValue(input)
func JSONTypeNullableValue[S, P any](input Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[database.JSONKind]] {
	return operationValue[S](querycore.JsonKindOperation, nullableJSONKindColumn(), operationArg(input))
}

// JSONContains evaluates JSON containment between identical payloads in the row phase.
//
// Example: expr.JSONContains(left, right)
func JSONContains[S, P any](left, right RowValue[S, value.JSON[P]]) RowExpression[S, bool] {
	return RowExpression[S, bool]{data: JSONContainsValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))).data}
}

// JSONContainsValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONContainsValue(left, right)
func JSONContainsValue[S, P any](left, right Selected[S, value.JSON[P]]) Selected[S, bool] {
	return operationValue[S](querycore.JsonContainsOperation, jsonBoolColumn(), operationArg(left), operationArg(right))
}

// JSONContainsNullable evaluates JSON containment between identical payloads in the row phase; SQL NULL propagates.
//
// Example: expr.JSONContainsNullable(left, right)
func JSONContainsNullable[S, P any](left, right RowValue[S, value.Nullable[value.JSON[P]]]) RowExpression[S, value.Nullable[bool]] {
	return RowExpression[S, value.Nullable[bool]]{data: JSONContainsNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))).data}
}

// JSONContainsNullableValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONContainsNullableValue(left, right)
func JSONContainsNullableValue[S, P any](left, right Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[bool]] {
	return operationValue[S](querycore.JsonContainsOperation, nullableJSONBoolColumn(), operationArg(left), operationArg(right))
}

// JSONContainedBy evaluates JSON containment between identical payloads in the row phase.
//
// Example: expr.JSONContainedBy(left, right)
func JSONContainedBy[S, P any](left, right RowValue[S, value.JSON[P]]) RowExpression[S, bool] {
	return RowExpression[S, bool]{data: JSONContainedByValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))).data}
}

// JSONContainedByValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONContainedByValue(left, right)
func JSONContainedByValue[S, P any](left, right Selected[S, value.JSON[P]]) Selected[S, bool] {
	return operationValue[S](querycore.JsonContainedByOperation, jsonBoolColumn(), operationArg(left), operationArg(right))
}

// JSONContainedByNullable evaluates JSON containment between identical payloads in the row phase; SQL NULL propagates.
//
// Example: expr.JSONContainedByNullable(left, right)
func JSONContainedByNullable[S, P any](left, right RowValue[S, value.Nullable[value.JSON[P]]]) RowExpression[S, value.Nullable[bool]] {
	return RowExpression[S, value.Nullable[bool]]{data: JSONContainedByNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))).data}
}

// JSONContainedByNullableValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONContainedByNullableValue(left, right)
func JSONContainedByNullableValue[S, P any](left, right Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[bool]] {
	return operationValue[S](querycore.JsonContainedByOperation, nullableJSONBoolColumn(), operationArg(left), operationArg(right))
}

// JSONLength evaluates JSON shape or safe array length in the row phase.
//
// Example: expr.JSONLength(input)
func JSONLength[S, P any](input RowValue[S, value.JSON[P]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(JSONLengthValue(AsSelected(rowInput(input))))
}

// JSONLengthValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONLengthValue(input)
func JSONLengthValue[S, P any](input Selected[S, value.JSON[P]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.JsonArrayLengthOperation, jsonLengthColumn(), operationArg(input))
}

// JSONLengthNullable evaluates JSON shape or safe array length in the row phase; SQL NULL propagates.
//
// Example: expr.JSONLengthNullable(input)
func JSONLengthNullable[S, P any](input RowValue[S, value.Nullable[value.JSON[P]]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(JSONLengthNullableValue(AsSelected(rowInput(input))))
}

// JSONLengthNullableValue preserves selected phase, including aggregates and windows.
//
// Example: expr.JSONLengthNullableValue(input)
func JSONLengthNullableValue[S, P any](input Selected[S, value.Nullable[value.JSON[P]]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.JsonArrayLengthOperation, jsonLengthColumn(), operationArg(input))
}
