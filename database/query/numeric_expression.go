package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Add adds two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
func Add[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(AddValue(rowInput(left).Value(), rowInput(right).Value()))
}

// AddValue is Add's selected-value counterpart for aggregate/window composition.
func AddValue[S any, V numericValue](left, right Expression[S, V]) Expression[S, V] {
	return operationValue[S](addOperation, left.codec, operationArg(left), operationArg(right))
}

// AddNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
func AddNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AddNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// AddNullableValue composes selected nullable numeric values.
func AddNullableValue[S any, V numericValue](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](addOperation, left.codec, operationArg(left), operationArg(right))
}

// Subtract subtracts two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
func Subtract[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(SubtractValue(rowInput(left).Value(), rowInput(right).Value()))
}

// SubtractValue is Subtract's selected-value counterpart for aggregate/window composition.
func SubtractValue[S any, V numericValue](left, right Expression[S, V]) Expression[S, V] {
	return operationValue[S](subtractOperation, left.codec, operationArg(left), operationArg(right))
}

// SubtractNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
func SubtractNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(SubtractNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// SubtractNullableValue composes selected nullable numeric values.
func SubtractNullableValue[S any, V numericValue](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](subtractOperation, left.codec, operationArg(left), operationArg(right))
}

// Multiply multiplies two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
func Multiply[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(MultiplyValue(rowInput(left).Value(), rowInput(right).Value()))
}

// MultiplyValue is Multiply's selected-value counterpart for aggregate/window composition.
func MultiplyValue[S any, V numericValue](left, right Expression[S, V]) Expression[S, V] {
	return operationValue[S](multiplyOperation, left.codec, operationArg(left), operationArg(right))
}

// MultiplyNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
func MultiplyNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(MultiplyNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// MultiplyNullableValue composes selected nullable numeric values.
func MultiplyNullableValue[S any, V numericValue](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](multiplyOperation, left.codec, operationArg(left), operationArg(right))
}

// Divide divides two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
func Divide[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(DivideValue(rowInput(left).Value(), rowInput(right).Value()))
}

// DivideValue is Divide's selected-value counterpart for aggregate/window composition.
func DivideValue[S any, V numericValue](left, right Expression[S, V]) Expression[S, V] {
	return operationValue[S](divideOperation, left.codec, operationArg(left), operationArg(right))
}

// DivideNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
func DivideNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(DivideNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// DivideNullableValue composes selected nullable numeric values.
func DivideNullableValue[S any, V numericValue](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](divideOperation, left.codec, operationArg(left), operationArg(right))
}

// Remainder takes the remainder of two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
func Remainder[S any, V exactNumber](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(RemainderValue(rowInput(left).Value(), rowInput(right).Value()))
}

// RemainderValue is Remainder's selected-value counterpart for aggregate/window composition.
func RemainderValue[S any, V exactNumber](left, right Expression[S, V]) Expression[S, V] {
	return operationValue[S](remainderOperation, left.codec, operationArg(left), operationArg(right))
}

// RemainderNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
func RemainderNullable[S any, V exactNumber](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(RemainderNullableValue(rowInput(left).Value(), rowInput(right).Value()))
}

// RemainderNullableValue composes selected nullable numeric values.
func RemainderNullableValue[S any, V exactNumber](left, right Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](remainderOperation, left.codec, operationArg(left), operationArg(right))
}

// Negate applies a numeric unary operation to a row value without changing its Go type.
func Negate[S any, V numericValue](input RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(NegateValue(rowInput(input).Value()))
}

// NegateValue applies Negate to a selected numeric value.
func NegateValue[S any, V numericValue](input Expression[S, V]) Expression[S, V] {
	return operationValue[S](negateOperation, input.codec, operationArg(input))
}

// NegateNullable propagates SQL NULL and retains the concrete numeric type.
func NegateNullable[S any, V numericValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(NegateNullableValue(rowInput(input).Value()))
}

// NegateNullableValue applies Negate to a selected nullable value.
func NegateNullableValue[S any, V numericValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](negateOperation, input.codec, operationArg(input))
}

// Abs applies a numeric unary operation to a row value without changing its Go type.
func Abs[S any, V numericValue](input RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(AbsValue(rowInput(input).Value()))
}

// AbsValue applies Abs to a selected numeric value.
func AbsValue[S any, V numericValue](input Expression[S, V]) Expression[S, V] {
	return operationValue[S](absOperation, input.codec, operationArg(input))
}

// AbsNullable propagates SQL NULL and retains the concrete numeric type.
func AbsNullable[S any, V numericValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AbsNullableValue(rowInput(input).Value()))
}

// AbsNullableValue applies Abs to a selected nullable value.
func AbsNullableValue[S any, V numericValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	return operationValue[S](absOperation, input.codec, operationArg(input))
}

// DecimalOf converts an exact integer/decimal input to SQL numeric; floating input is excluded.
func DecimalOf[S any, V exactNumber](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(DecimalValue(rowInput(input).Value()))
}

// DecimalValue is the selected-value counterpart of DecimalOf.
func DecimalValue[S any, V exactNumber](input Expression[S, V]) Expression[S, decimal.Decimal] {
	return operationValue[S](castOperation, codec.Decimal(), operationArg(input))
}

// DecimalNullable converts a nullable row input without removing NULL.
func DecimalNullable[S any, V exactNumber](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(DecimalNullableValue(rowInput(input).Value()))
}

// DecimalNullableValue converts a selected nullable input without removing NULL.
func DecimalNullableValue[S any, V exactNumber](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](castOperation, codec.Nullable(codec.Decimal()), operationArg(input))
}

// FloatOf explicitly converts a numeric input to approximate double precision.
func FloatOf[S any, V numericValue](input RowValue[S, V]) OrderedRowExpression[S, float64] {
	return orderedRow(FloatValue(rowInput(input).Value()))
}

// FloatValue is the selected-value counterpart of FloatOf.
func FloatValue[S any, V numericValue](input Expression[S, V]) Expression[S, float64] {
	return operationValue[S](castOperation, codec.Float[float64](), operationArg(input))
}

// FloatNullable converts a nullable row input without removing NULL.
func FloatNullable[S any, V numericValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, float64] {
	return nullableOrderedRow(FloatNullableValue(rowInput(input).Value()))
}

// FloatNullableValue converts a selected nullable input without removing NULL.
func FloatNullableValue[S any, V numericValue](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[float64]] {
	return operationValue[S](castOperation, codec.Nullable(codec.Float[float64]()), operationArg(input))
}
