package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Add adds two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
//
// Example: expr.Add(left, right)
func Add[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(AddValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// AddValue is Add's selected-value counterpart for aggregate/window composition.
//
// Example: expr.AddValue(left, right)
func AddValue[S any, V numericValue](left, right Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.AddOperation, left.data.column, operationArg(left), operationArg(right))
}

// AddNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
//
// Example: expr.AddNullable(left, right)
func AddNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AddNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// AddNullableValue composes selected nullable numeric values.
//
// Example: expr.AddNullableValue(left, right)
func AddNullableValue[S any, V numericValue](left, right Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.AddOperation, left.data.column, operationArg(left), operationArg(right))
}

// Subtract subtracts two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
//
// Example: expr.Subtract(left, right)
func Subtract[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(SubtractValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// SubtractValue is Subtract's selected-value counterpart for aggregate/window composition.
//
// Example: expr.SubtractValue(left, right)
func SubtractValue[S any, V numericValue](left, right Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.SubtractOperation, left.data.column, operationArg(left), operationArg(right))
}

// SubtractNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
//
// Example: expr.SubtractNullable(left, right)
func SubtractNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(SubtractNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// SubtractNullableValue composes selected nullable numeric values.
//
// Example: expr.SubtractNullableValue(left, right)
func SubtractNullableValue[S any, V numericValue](left, right Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.SubtractOperation, left.data.column, operationArg(left), operationArg(right))
}

// Multiply multiplies two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
//
// Example: expr.Multiply(left, right)
func Multiply[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(MultiplyValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// MultiplyValue is Multiply's selected-value counterpart for aggregate/window composition.
//
// Example: expr.MultiplyValue(left, right)
func MultiplyValue[S any, V numericValue](left, right Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.MultiplyOperation, left.data.column, operationArg(left), operationArg(right))
}

// MultiplyNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
//
// Example: expr.MultiplyNullable(left, right)
func MultiplyNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(MultiplyNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// MultiplyNullableValue composes selected nullable numeric values.
//
// Example: expr.MultiplyNullableValue(left, right)
func MultiplyNullableValue[S any, V numericValue](left, right Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.MultiplyOperation, left.data.column, operationArg(left), operationArg(right))
}

// Divide divides two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
//
// Example: expr.Divide(left, right)
func Divide[S any, V numericValue](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(DivideValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// DivideValue is Divide's selected-value counterpart for aggregate/window composition.
//
// Example: expr.DivideValue(left, right)
func DivideValue[S any, V numericValue](left, right Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.DivideOperation, left.data.column, operationArg(left), operationArg(right))
}

// DivideNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
//
// Example: expr.DivideNullable(left, right)
func DivideNullable[S any, V numericValue](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(DivideNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// DivideNullableValue composes selected nullable numeric values.
//
// Example: expr.DivideNullableValue(left, right)
func DivideNullableValue[S any, V numericValue](left, right Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.DivideOperation, left.data.column, operationArg(left), operationArg(right))
}

// Remainder takes the remainder of two row values with the same owner and concrete numeric type.
// Arithmetic uses the codec's SQL representation; division by zero and numeric
// overflow are database errors. Integer division truncates toward zero.
//
// Example: expr.Remainder(left, right)
func Remainder[S any, V exactNumber](left, right RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(RemainderValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// RemainderValue is Remainder's selected-value counterpart for aggregate/window composition.
//
// Example: expr.RemainderValue(left, right)
func RemainderValue[S any, V exactNumber](left, right Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.RemainderOperation, left.data.column, operationArg(left), operationArg(right))
}

// RemainderNullable propagates SQL NULL. Widen a non-null operand with NullableRow.
//
// Example: expr.RemainderNullable(left, right)
func RemainderNullable[S any, V exactNumber](left, right RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(RemainderNullableValue(AsSelected(rowInput(left)), AsSelected(rowInput(right))))
}

// RemainderNullableValue composes selected nullable numeric values.
//
// Example: expr.RemainderNullableValue(left, right)
func RemainderNullableValue[S any, V exactNumber](left, right Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.RemainderOperation, left.data.column, operationArg(left), operationArg(right))
}

// Negate applies a numeric unary operation to a row value without changing its Go type.
//
// Example: expr.Negate(input)
func Negate[S any, V numericValue](input RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(NegateValue(AsSelected(rowInput(input))))
}

// NegateValue applies Negate to a selected numeric value.
//
// Example: expr.NegateValue(input)
func NegateValue[S any, V numericValue](input Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.NegateOperation, input.data.column, operationArg(input))
}

// NegateNullable propagates SQL NULL and retains the concrete numeric type.
//
// Example: expr.NegateNullable(input)
func NegateNullable[S any, V numericValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(NegateNullableValue(AsSelected(rowInput(input))))
}

// NegateNullableValue applies Negate to a selected nullable value.
//
// Example: expr.NegateNullableValue(input)
func NegateNullableValue[S any, V numericValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.NegateOperation, input.data.column, operationArg(input))
}

// Abs applies a numeric unary operation to a row value without changing its Go type.
//
// Example: expr.Abs(input)
func Abs[S any, V numericValue](input RowValue[S, V]) OrderedRowExpression[S, V] {
	return orderedRow(AbsValue(AsSelected(rowInput(input))))
}

// AbsValue applies Abs to a selected numeric value.
//
// Example: expr.AbsValue(input)
func AbsValue[S any, V numericValue](input Selected[S, V]) Selected[S, V] {
	return operationValue[S](querycore.AbsOperation, input.data.column, operationArg(input))
}

// AbsNullable propagates SQL NULL and retains the concrete numeric type.
//
// Example: expr.AbsNullable(input)
func AbsNullable[S any, V numericValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, V] {
	return nullableOrderedRow(AbsNullableValue(AsSelected(rowInput(input))))
}

// AbsNullableValue applies Abs to a selected nullable value.
//
// Example: expr.AbsNullableValue(input)
func AbsNullableValue[S any, V numericValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return operationValue[S](querycore.AbsOperation, input.data.column, operationArg(input))
}

// DecimalOf converts an exact integer/decimal input to SQL numeric; floating input is excluded.
//
// Example: expr.DecimalOf(input)
func DecimalOf[S any, V exactNumber](input RowValue[S, V]) OrderedRowExpression[S, decimal.Decimal] {
	return orderedRow(DecimalValue(AsSelected(rowInput(input))))
}

// DecimalValue is the selected-value counterpart of DecimalOf.
//
// Example: expr.DecimalValue(input)
func DecimalValue[S any, V exactNumber](input Selected[S, V]) Selected[S, decimal.Decimal] {
	return operationValue[S](querycore.CastOperation, columnCodec(codec.Decimal()), operationArg(input))
}

// DecimalNullable converts a nullable row input without removing NULL.
//
// Example: expr.DecimalNullable(input)
func DecimalNullable[S any, V exactNumber](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, decimal.Decimal] {
	return nullableOrderedRow(DecimalNullableValue(AsSelected(rowInput(input))))
}

// DecimalNullableValue converts a selected nullable input without removing NULL.
//
// Example: expr.DecimalNullableValue(input)
func DecimalNullableValue[S any, V exactNumber](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[decimal.Decimal]] {
	return operationValue[S](querycore.CastOperation, columnCodec(codec.Nullable(codec.Decimal())), operationArg(input))
}

// FloatOf explicitly converts a numeric input to approximate double precision.
//
// Example: expr.FloatOf(input)
func FloatOf[S any, V numericValue](input RowValue[S, V]) OrderedRowExpression[S, float64] {
	return orderedRow(FloatValue(AsSelected(rowInput(input))))
}

// FloatValue is the selected-value counterpart of FloatOf.
//
// Example: expr.FloatValue(input)
func FloatValue[S any, V numericValue](input Selected[S, V]) Selected[S, float64] {
	return operationValue[S](querycore.CastOperation, columnCodec(codec.Float[float64]()), operationArg(input))
}

// FloatNullable converts a nullable row input without removing NULL.
//
// Example: expr.FloatNullable(input)
func FloatNullable[S any, V numericValue](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, float64] {
	return nullableOrderedRow(FloatNullableValue(AsSelected(rowInput(input))))
}

// FloatNullableValue converts a selected nullable input without removing NULL.
//
// Example: expr.FloatNullableValue(input)
func FloatNullableValue[S any, V numericValue](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[float64]] {
	return operationValue[S](querycore.CastOperation, columnCodec(codec.Nullable(codec.Float[float64]())), operationArg(input))
}
