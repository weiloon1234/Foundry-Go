package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Text calculations return ordinary strings, preserving query scope and NULL
// separately from the input's named/domain string type and validation rules.
// Lower lowercases text using the database collation.
func Lower[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(LowerValue(rowInput(input).Value()))
}

// LowerValue applies Lower to selected expressions, including aggregates/windows.
func LowerValue[S any, V ~string](input Expression[S, V]) Expression[S, string] {
	return operationValue[S](lowerOperation, codec.String[string](), operationArg(input))
}

// LowerNullable lowercases text using the database collation, propagating SQL NULL.
func LowerNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(LowerNullableValue(rowInput(input).Value()))
}

// LowerNullableValue applies LowerNullable to selected expressions, including aggregates/windows.
func LowerNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](lowerOperation, codec.Nullable(codec.String[string]()), operationArg(input))
}

// Upper uppercases text using the database collation.
func Upper[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(UpperValue(rowInput(input).Value()))
}

// UpperValue applies Upper to selected expressions, including aggregates/windows.
func UpperValue[S any, V ~string](input Expression[S, V]) Expression[S, string] {
	return operationValue[S](upperOperation, codec.String[string](), operationArg(input))
}

// UpperNullable uppercases text using the database collation, propagating SQL NULL.
func UpperNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(UpperNullableValue(rowInput(input).Value()))
}

// UpperNullableValue applies UpperNullable to selected expressions, including aggregates/windows.
func UpperNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](upperOperation, codec.Nullable(codec.String[string]()), operationArg(input))
}

// Trim removes ordinary spaces from both ends.
func Trim[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(TrimValue(rowInput(input).Value()))
}

// TrimValue applies Trim to selected expressions, including aggregates/windows.
func TrimValue[S any, V ~string](input Expression[S, V]) Expression[S, string] {
	return operationValue[S](trimOperation, codec.String[string](), operationArg(input))
}

// TrimNullable removes ordinary spaces from both ends, propagating SQL NULL.
func TrimNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimNullableValue(rowInput(input).Value()))
}

// TrimNullableValue applies TrimNullable to selected expressions, including aggregates/windows.
func TrimNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](trimOperation, codec.Nullable(codec.String[string]()), operationArg(input))
}

// TrimLeft removes ordinary spaces from the start.
func TrimLeft[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(TrimLeftValue(rowInput(input).Value()))
}

// TrimLeftValue applies TrimLeft to selected expressions, including aggregates/windows.
func TrimLeftValue[S any, V ~string](input Expression[S, V]) Expression[S, string] {
	return operationValue[S](trimLeftOperation, codec.String[string](), operationArg(input))
}

// TrimLeftNullable removes ordinary spaces from the start, propagating SQL NULL.
func TrimLeftNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimLeftNullableValue(rowInput(input).Value()))
}

// TrimLeftNullableValue applies TrimLeftNullable to selected expressions, including aggregates/windows.
func TrimLeftNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](trimLeftOperation, codec.Nullable(codec.String[string]()), operationArg(input))
}

// TrimRight removes ordinary spaces from the end.
func TrimRight[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(TrimRightValue(rowInput(input).Value()))
}

// TrimRightValue applies TrimRight to selected expressions, including aggregates/windows.
func TrimRightValue[S any, V ~string](input Expression[S, V]) Expression[S, string] {
	return operationValue[S](trimRightOperation, codec.String[string](), operationArg(input))
}

// TrimRightNullable removes ordinary spaces from the end, propagating SQL NULL.
func TrimRightNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimRightNullableValue(rowInput(input).Value()))
}

// TrimRightNullableValue applies TrimRightNullable to selected expressions, including aggregates/windows.
func TrimRightNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](trimRightOperation, codec.Nullable(codec.String[string]()), operationArg(input))
}

// TrimChars removes characters in the second argument from both ends.
func TrimChars[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(TrimCharsValue(rowInput(input).Value(), rowInput(other).Value()))
}

// TrimCharsValue applies TrimChars to selected expressions, including aggregates/windows.
func TrimCharsValue[S any, V, W ~string](input Expression[S, V], other Expression[S, W]) Expression[S, string] {
	return operationValue[S](trimOperation, codec.String[string](), operationArg(input), operationArg(other))
}

// TrimCharsNullable removes characters in the second argument from both ends, propagating SQL NULL.
func TrimCharsNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimCharsNullableValue(rowInput(input).Value(), rowInput(other).Value()))
}

// TrimCharsNullableValue applies TrimCharsNullable to selected expressions, including aggregates/windows.
func TrimCharsNullableValue[S any, V, W ~string](input Expression[S, value.Nullable[V]], other Expression[S, value.Nullable[W]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](trimOperation, codec.Nullable(codec.String[string]()), operationArg(input), operationArg(other))
}

// TrimLeftChars removes characters in the second argument from the start.
func TrimLeftChars[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(TrimLeftCharsValue(rowInput(input).Value(), rowInput(other).Value()))
}

// TrimLeftCharsValue applies TrimLeftChars to selected expressions, including aggregates/windows.
func TrimLeftCharsValue[S any, V, W ~string](input Expression[S, V], other Expression[S, W]) Expression[S, string] {
	return operationValue[S](trimLeftOperation, codec.String[string](), operationArg(input), operationArg(other))
}

// TrimLeftCharsNullable removes characters in the second argument from the start, propagating SQL NULL.
func TrimLeftCharsNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimLeftCharsNullableValue(rowInput(input).Value(), rowInput(other).Value()))
}

// TrimLeftCharsNullableValue applies TrimLeftCharsNullable to selected expressions, including aggregates/windows.
func TrimLeftCharsNullableValue[S any, V, W ~string](input Expression[S, value.Nullable[V]], other Expression[S, value.Nullable[W]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](trimLeftOperation, codec.Nullable(codec.String[string]()), operationArg(input), operationArg(other))
}

// TrimRightChars removes characters in the second argument from the end.
func TrimRightChars[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(TrimRightCharsValue(rowInput(input).Value(), rowInput(other).Value()))
}

// TrimRightCharsValue applies TrimRightChars to selected expressions, including aggregates/windows.
func TrimRightCharsValue[S any, V, W ~string](input Expression[S, V], other Expression[S, W]) Expression[S, string] {
	return operationValue[S](trimRightOperation, codec.String[string](), operationArg(input), operationArg(other))
}

// TrimRightCharsNullable removes characters in the second argument from the end, propagating SQL NULL.
func TrimRightCharsNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimRightCharsNullableValue(rowInput(input).Value(), rowInput(other).Value()))
}

// TrimRightCharsNullableValue applies TrimRightCharsNullable to selected expressions, including aggregates/windows.
func TrimRightCharsNullableValue[S any, V, W ~string](input Expression[S, value.Nullable[V]], other Expression[S, value.Nullable[W]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](trimRightOperation, codec.Nullable(codec.String[string]()), operationArg(input), operationArg(other))
}

// Concat concatenates text with SQL's strict NULL semantics.
func Concat[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(ConcatValue(rowInput(input).Value(), rowInput(other).Value()))
}

// ConcatValue applies Concat to selected expressions, including aggregates/windows.
func ConcatValue[S any, V, W ~string](input Expression[S, V], other Expression[S, W]) Expression[S, string] {
	return operationValue[S](concatOperation, codec.String[string](), operationArg(input), operationArg(other))
}

// ConcatNullable concatenates text with SQL's strict NULL semantics, propagating SQL NULL.
func ConcatNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(ConcatNullableValue(rowInput(input).Value(), rowInput(other).Value()))
}

// ConcatNullableValue applies ConcatNullable to selected expressions, including aggregates/windows.
func ConcatNullableValue[S any, V, W ~string](input Expression[S, value.Nullable[V]], other Expression[S, value.Nullable[W]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](concatOperation, codec.Nullable(codec.String[string]()), operationArg(input), operationArg(other))
}

// Replace replaces literal occurrences of its second argument with its third.
func Replace[S any, V, W, X ~string](input RowValue[S, V], other RowValue[S, W], replacement RowValue[S, X]) TextRowExpression[S, string] {
	return textRow(ReplaceValue(rowInput(input).Value(), rowInput(other).Value(), rowInput(replacement).Value()))
}

// ReplaceValue applies Replace to selected expressions, including aggregates/windows.
func ReplaceValue[S any, V, W, X ~string](input Expression[S, V], other Expression[S, W], replacement Expression[S, X]) Expression[S, string] {
	return operationValue[S](replaceOperation, codec.String[string](), operationArg(input), operationArg(other), operationArg(replacement))
}

// ReplaceNullable replaces literal occurrences of its second argument with its third, propagating SQL NULL.
func ReplaceNullable[S any, V, W, X ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]], replacement RowValue[S, value.Nullable[X]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(ReplaceNullableValue(rowInput(input).Value(), rowInput(other).Value(), rowInput(replacement).Value()))
}

// ReplaceNullableValue applies ReplaceNullable to selected expressions, including aggregates/windows.
func ReplaceNullableValue[S any, V, W, X ~string](input Expression[S, value.Nullable[V]], other Expression[S, value.Nullable[W]], replacement Expression[S, value.Nullable[X]]) Expression[S, value.Nullable[string]] {
	return operationValue[S](replaceOperation, codec.Nullable(codec.String[string]()), operationArg(input), operationArg(other), operationArg(replacement))
}

// Length counts Unicode characters rather than encoded bytes.
func Length[S any, V ~string](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(LengthValue(rowInput(input).Value()))
}

// LengthValue applies Length to selected expressions, including aggregates/windows.
func LengthValue[S any, V ~string](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](lengthOperation, codec.Signed[int64](), operationArg(input))
}

// LengthNullable counts Unicode characters rather than encoded bytes, propagating SQL NULL.
func LengthNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(LengthNullableValue(rowInput(input).Value()))
}

// LengthNullableValue applies LengthNullable to selected expressions, including aggregates/windows.
func LengthNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](lengthOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// OctetLength counts encoded bytes.
func OctetLength[S any, V ~string](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(OctetLengthValue(rowInput(input).Value()))
}

// OctetLengthValue applies OctetLength to selected expressions, including aggregates/windows.
func OctetLengthValue[S any, V ~string](input Expression[S, V]) Expression[S, int64] {
	return operationValue[S](octetLengthOperation, codec.Signed[int64](), operationArg(input))
}

// OctetLengthNullable counts encoded bytes, propagating SQL NULL.
func OctetLengthNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(OctetLengthNullableValue(rowInput(input).Value()))
}

// OctetLengthNullableValue applies OctetLengthNullable to selected expressions, including aggregates/windows.
func OctetLengthNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[int64]] {
	return operationValue[S](octetLengthOperation, codec.Nullable(codec.Signed[int64]()), operationArg(input))
}

// ConcatWS joins text with a bound, non-null separator. SQL NULL inputs are
// skipped; empty strings are retained. The result is always a non-null string.
func ConcatWS[S any, V ~string](separator string, first RowValue[S, V], rest ...RowValue[S, V]) TextRowExpression[S, string] {
	items := make([]Expression[S, V], len(rest))
	for i, v := range rest {
		items[i] = rowInput(v).Value()
	}
	return textRow(ConcatWSValue(separator, rowInput(first).Value(), items...))
}

// ConcatWSValue accepts selected text values and returns a non-null string.
func ConcatWSValue[S any, V ~string](separator string, first Expression[S, V], rest ...Expression[S, V]) Expression[S, string] {
	args := make([]operationArgument, 0, len(rest)+2)
	args = append(args, operationArg(parameterExpression[S](separator, codec.String[string]()).Value()), operationArg(first))
	for _, v := range rest {
		args = append(args, operationArg(v))
	}
	return operationValue[S](concatWSOperation, codec.String[string](), args...)
}

// ConcatWSNullable joins text with a bound, non-null separator. SQL NULL inputs are
// skipped; empty strings are retained. The result is always a non-null string.
func ConcatWSNullable[S any, V ~string](separator string, first RowValue[S, value.Nullable[V]], rest ...RowValue[S, value.Nullable[V]]) TextRowExpression[S, string] {
	items := make([]Expression[S, value.Nullable[V]], len(rest))
	for i, v := range rest {
		items[i] = rowInput(v).Value()
	}
	return textRow(ConcatWSNullableValue(separator, rowInput(first).Value(), items...))
}

// ConcatWSNullableValue accepts selected text values and returns a non-null string.
func ConcatWSNullableValue[S any, V ~string](separator string, first Expression[S, value.Nullable[V]], rest ...Expression[S, value.Nullable[V]]) Expression[S, string] {
	args := make([]operationArgument, 0, len(rest)+2)
	args = append(args, operationArg(parameterExpression[S](separator, codec.String[string]()).Value()), operationArg(first))
	for _, v := range rest {
		args = append(args, operationArg(v))
	}
	return operationValue[S](concatWSOperation, codec.String[string](), args...)
}

// Substring selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. A negative count is rejected before execution.
func Substring[S any, V ~string](input RowValue[S, V], start int32, count int32) TextRowExpression[S, string] {
	return textRow(SubstringValue(rowInput(input).Value(), start, count))
}

// SubstringValue accepts a selected text value and bound character positions.
func SubstringValue[S any, V ~string](input Expression[S, V], start int32, count int32) Expression[S, string] {
	args := []operationArgument{operationArg(input), operationArg(parameterExpression[S](start, codec.Signed[int32]()).Value())}
	args = append(args, operationArg(parameterExpression[S](count, codec.Signed[int32]()).Value()))
	result := operationValue[S](substringOperation, codec.String[string](), args...)
	if count < 0 {
		n := result.node.(operationNode)
		n.err = fault.New(fault.Invalid, "substring count cannot be negative")
		result.node = n
	}
	return result
}

// SubstringNullable selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. A negative count is rejected before execution.
func SubstringNullable[S any, V ~string](input RowValue[S, value.Nullable[V]], start int32, count int32) NullableTextRowExpression[S, string] {
	return nullableTextRow(SubstringNullableValue(rowInput(input).Value(), start, count))
}

// SubstringNullableValue accepts a selected text value and bound character positions.
func SubstringNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]], start int32, count int32) Expression[S, value.Nullable[string]] {
	args := []operationArgument{operationArg(input), operationArg(parameterExpression[S](start, codec.Signed[int32]()).Value())}
	args = append(args, operationArg(parameterExpression[S](count, codec.Signed[int32]()).Value()))
	result := operationValue[S](substringOperation, codec.Nullable(codec.String[string]()), args...)
	if count < 0 {
		n := result.node.(operationNode)
		n.err = fault.New(fault.Invalid, "substring count cannot be negative")
		result.node = n
	}
	return result
}

// SubstringFrom selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. It selects through the end.
func SubstringFrom[S any, V ~string](input RowValue[S, V], start int32) TextRowExpression[S, string] {
	return textRow(SubstringFromValue(rowInput(input).Value(), start))
}

// SubstringFromValue accepts a selected text value and bound character positions.
func SubstringFromValue[S any, V ~string](input Expression[S, V], start int32) Expression[S, string] {
	args := []operationArgument{operationArg(input), operationArg(parameterExpression[S](start, codec.Signed[int32]()).Value())}

	result := operationValue[S](substringOperation, codec.String[string](), args...)

	return result
}

// SubstringFromNullable selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. It selects through the end.
func SubstringFromNullable[S any, V ~string](input RowValue[S, value.Nullable[V]], start int32) NullableTextRowExpression[S, string] {
	return nullableTextRow(SubstringFromNullableValue(rowInput(input).Value(), start))
}

// SubstringFromNullableValue accepts a selected text value and bound character positions.
func SubstringFromNullableValue[S any, V ~string](input Expression[S, value.Nullable[V]], start int32) Expression[S, value.Nullable[string]] {
	args := []operationArgument{operationArg(input), operationArg(parameterExpression[S](start, codec.Signed[int32]()).Value())}

	result := operationValue[S](substringOperation, codec.Nullable(codec.String[string]()), args...)

	return result
}

// SubstringAt uses typed integer expressions as positions. Database negative-count
// and SQL-integer overflow errors remain runtime errors. Positions are non-null.
func SubstringAt[S any, V ~string, I integerNumber](input RowValue[S, V], start, count RowValue[S, I]) TextRowExpression[S, string] {
	return textRow(SubstringAtValue(rowInput(input).Value(), rowInput(start).Value(), rowInput(count).Value()))
}

// SubstringAtValue retains selected text/integer scopes for dynamic positions.
func SubstringAtValue[S any, V ~string, I integerNumber](input Expression[S, V], start, count Expression[S, I]) Expression[S, string] {
	return operationValue[S](substringOperation, codec.String[string](), operationArg(input), operationArg(start), operationArg(count))
}

// SubstringAtNullable uses typed integer expressions as positions. Database negative-count
// and SQL-integer overflow errors remain runtime errors. Positions are non-null.
func SubstringAtNullable[S any, V ~string, I integerNumber](input RowValue[S, value.Nullable[V]], start, count RowValue[S, I]) NullableTextRowExpression[S, string] {
	return nullableTextRow(SubstringAtNullableValue(rowInput(input).Value(), rowInput(start).Value(), rowInput(count).Value()))
}

// SubstringAtNullableValue retains selected text/integer scopes for dynamic positions.
func SubstringAtNullableValue[S any, V ~string, I integerNumber](input Expression[S, value.Nullable[V]], start, count Expression[S, I]) Expression[S, value.Nullable[string]] {
	return operationValue[S](substringOperation, codec.Nullable(codec.String[string]()), operationArg(input), operationArg(start), operationArg(count))
}
