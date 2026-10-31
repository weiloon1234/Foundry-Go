package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Text calculations return ordinary strings, preserving query scope and NULL
// separately from the input's named/domain string type and validation rules.
// Lower lowercases text using the database collation.
//
// Example: expr.Lower(input)
func Lower[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(LowerValue(AsSelected(rowInput(input))))
}

// LowerValue applies Lower to selected expressions, including aggregates/windows.
//
// Example: expr.LowerValue(input)
func LowerValue[S any, V ~string](input Selected[S, V]) Selected[S, string] {
	return operationValue[S](querycore.LowerOperation, columnCodec(codec.String[string]()), operationArg(input))
}

// LowerNullable lowercases text using the database collation, propagating SQL NULL.
//
// Example: expr.LowerNullable(input)
func LowerNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(LowerNullableValue(AsSelected(rowInput(input))))
}

// LowerNullableValue applies LowerNullable to selected expressions, including aggregates/windows.
//
// Example: expr.LowerNullableValue(input)
func LowerNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.LowerOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input))
}

// Upper uppercases text using the database collation.
//
// Example: expr.Upper(input)
func Upper[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(UpperValue(AsSelected(rowInput(input))))
}

// UpperValue applies Upper to selected expressions, including aggregates/windows.
//
// Example: expr.UpperValue(input)
func UpperValue[S any, V ~string](input Selected[S, V]) Selected[S, string] {
	return operationValue[S](querycore.UpperOperation, columnCodec(codec.String[string]()), operationArg(input))
}

// UpperNullable uppercases text using the database collation, propagating SQL NULL.
//
// Example: expr.UpperNullable(input)
func UpperNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(UpperNullableValue(AsSelected(rowInput(input))))
}

// UpperNullableValue applies UpperNullable to selected expressions, including aggregates/windows.
//
// Example: expr.UpperNullableValue(input)
func UpperNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.UpperOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input))
}

// Trim removes ordinary spaces from both ends.
//
// Example: expr.Trim(input)
func Trim[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(TrimValue(AsSelected(rowInput(input))))
}

// TrimValue applies Trim to selected expressions, including aggregates/windows.
//
// Example: expr.TrimValue(input)
func TrimValue[S any, V ~string](input Selected[S, V]) Selected[S, string] {
	return operationValue[S](querycore.TrimOperation, columnCodec(codec.String[string]()), operationArg(input))
}

// TrimNullable removes ordinary spaces from both ends, propagating SQL NULL.
//
// Example: expr.TrimNullable(input)
func TrimNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimNullableValue(AsSelected(rowInput(input))))
}

// TrimNullableValue applies TrimNullable to selected expressions, including aggregates/windows.
//
// Example: expr.TrimNullableValue(input)
func TrimNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.TrimOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input))
}

// TrimLeft removes ordinary spaces from the start.
//
// Example: expr.TrimLeft(input)
func TrimLeft[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(TrimLeftValue(AsSelected(rowInput(input))))
}

// TrimLeftValue applies TrimLeft to selected expressions, including aggregates/windows.
//
// Example: expr.TrimLeftValue(input)
func TrimLeftValue[S any, V ~string](input Selected[S, V]) Selected[S, string] {
	return operationValue[S](querycore.TrimLeftOperation, columnCodec(codec.String[string]()), operationArg(input))
}

// TrimLeftNullable removes ordinary spaces from the start, propagating SQL NULL.
//
// Example: expr.TrimLeftNullable(input)
func TrimLeftNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimLeftNullableValue(AsSelected(rowInput(input))))
}

// TrimLeftNullableValue applies TrimLeftNullable to selected expressions, including aggregates/windows.
//
// Example: expr.TrimLeftNullableValue(input)
func TrimLeftNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.TrimLeftOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input))
}

// TrimRight removes ordinary spaces from the end.
//
// Example: expr.TrimRight(input)
func TrimRight[S any, V ~string](input RowValue[S, V]) TextRowExpression[S, string] {
	return textRow(TrimRightValue(AsSelected(rowInput(input))))
}

// TrimRightValue applies TrimRight to selected expressions, including aggregates/windows.
//
// Example: expr.TrimRightValue(input)
func TrimRightValue[S any, V ~string](input Selected[S, V]) Selected[S, string] {
	return operationValue[S](querycore.TrimRightOperation, columnCodec(codec.String[string]()), operationArg(input))
}

// TrimRightNullable removes ordinary spaces from the end, propagating SQL NULL.
//
// Example: expr.TrimRightNullable(input)
func TrimRightNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimRightNullableValue(AsSelected(rowInput(input))))
}

// TrimRightNullableValue applies TrimRightNullable to selected expressions, including aggregates/windows.
//
// Example: expr.TrimRightNullableValue(input)
func TrimRightNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.TrimRightOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input))
}

// TrimChars removes characters in the second argument from both ends.
//
// Example: expr.TrimChars(input, other)
func TrimChars[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(TrimCharsValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// TrimCharsValue applies TrimChars to selected expressions, including aggregates/windows.
//
// Example: expr.TrimCharsValue(input, other)
func TrimCharsValue[S any, V, W ~string](input Selected[S, V], other Selected[S, W]) Selected[S, string] {
	return operationValue[S](querycore.TrimOperation, columnCodec(codec.String[string]()), operationArg(input), operationArg(other))
}

// TrimCharsNullable removes characters in the second argument from both ends, propagating SQL NULL.
//
// Example: expr.TrimCharsNullable(input, other)
func TrimCharsNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimCharsNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// TrimCharsNullableValue applies TrimCharsNullable to selected expressions, including aggregates/windows.
//
// Example: expr.TrimCharsNullableValue(input, other)
func TrimCharsNullableValue[S any, V, W ~string](input Selected[S, value.Nullable[V]], other Selected[S, value.Nullable[W]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.TrimOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input), operationArg(other))
}

// TrimLeftChars removes characters in the second argument from the start.
//
// Example: expr.TrimLeftChars(input, other)
func TrimLeftChars[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(TrimLeftCharsValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// TrimLeftCharsValue applies TrimLeftChars to selected expressions, including aggregates/windows.
//
// Example: expr.TrimLeftCharsValue(input, other)
func TrimLeftCharsValue[S any, V, W ~string](input Selected[S, V], other Selected[S, W]) Selected[S, string] {
	return operationValue[S](querycore.TrimLeftOperation, columnCodec(codec.String[string]()), operationArg(input), operationArg(other))
}

// TrimLeftCharsNullable removes characters in the second argument from the start, propagating SQL NULL.
//
// Example: expr.TrimLeftCharsNullable(input, other)
func TrimLeftCharsNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimLeftCharsNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// TrimLeftCharsNullableValue applies TrimLeftCharsNullable to selected expressions, including aggregates/windows.
//
// Example: expr.TrimLeftCharsNullableValue(input, other)
func TrimLeftCharsNullableValue[S any, V, W ~string](input Selected[S, value.Nullable[V]], other Selected[S, value.Nullable[W]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.TrimLeftOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input), operationArg(other))
}

// TrimRightChars removes characters in the second argument from the end.
//
// Example: expr.TrimRightChars(input, other)
func TrimRightChars[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(TrimRightCharsValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// TrimRightCharsValue applies TrimRightChars to selected expressions, including aggregates/windows.
//
// Example: expr.TrimRightCharsValue(input, other)
func TrimRightCharsValue[S any, V, W ~string](input Selected[S, V], other Selected[S, W]) Selected[S, string] {
	return operationValue[S](querycore.TrimRightOperation, columnCodec(codec.String[string]()), operationArg(input), operationArg(other))
}

// TrimRightCharsNullable removes characters in the second argument from the end, propagating SQL NULL.
//
// Example: expr.TrimRightCharsNullable(input, other)
func TrimRightCharsNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(TrimRightCharsNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// TrimRightCharsNullableValue applies TrimRightCharsNullable to selected expressions, including aggregates/windows.
//
// Example: expr.TrimRightCharsNullableValue(input, other)
func TrimRightCharsNullableValue[S any, V, W ~string](input Selected[S, value.Nullable[V]], other Selected[S, value.Nullable[W]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.TrimRightOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input), operationArg(other))
}

// Concat concatenates text with SQL's strict NULL semantics.
//
// Example: expr.Concat(input, other)
func Concat[S any, V, W ~string](input RowValue[S, V], other RowValue[S, W]) TextRowExpression[S, string] {
	return textRow(ConcatValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// ConcatValue applies Concat to selected expressions, including aggregates/windows.
//
// Example: expr.ConcatValue(input, other)
func ConcatValue[S any, V, W ~string](input Selected[S, V], other Selected[S, W]) Selected[S, string] {
	return operationValue[S](querycore.ConcatOperation, columnCodec(codec.String[string]()), operationArg(input), operationArg(other))
}

// ConcatNullable concatenates text with SQL's strict NULL semantics, propagating SQL NULL.
//
// Example: expr.ConcatNullable(input, other)
func ConcatNullable[S any, V, W ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(ConcatNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(other))))
}

// ConcatNullableValue applies ConcatNullable to selected expressions, including aggregates/windows.
//
// Example: expr.ConcatNullableValue(input, other)
func ConcatNullableValue[S any, V, W ~string](input Selected[S, value.Nullable[V]], other Selected[S, value.Nullable[W]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.ConcatOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input), operationArg(other))
}

// Replace replaces literal occurrences of its second argument with its third.
//
// Example: expr.Replace(input, other, replacement)
func Replace[S any, V, W, X ~string](input RowValue[S, V], other RowValue[S, W], replacement RowValue[S, X]) TextRowExpression[S, string] {
	return textRow(ReplaceValue(AsSelected(rowInput(input)), AsSelected(rowInput(other)), AsSelected(rowInput(replacement))))
}

// ReplaceValue applies Replace to selected expressions, including aggregates/windows.
//
// Example: expr.ReplaceValue(input, other, replacement)
func ReplaceValue[S any, V, W, X ~string](input Selected[S, V], other Selected[S, W], replacement Selected[S, X]) Selected[S, string] {
	return operationValue[S](querycore.ReplaceOperation, columnCodec(codec.String[string]()), operationArg(input), operationArg(other), operationArg(replacement))
}

// ReplaceNullable replaces literal occurrences of its second argument with its third, propagating SQL NULL.
//
// Example: expr.ReplaceNullable(input, other, replacement)
func ReplaceNullable[S any, V, W, X ~string](input RowValue[S, value.Nullable[V]], other RowValue[S, value.Nullable[W]], replacement RowValue[S, value.Nullable[X]]) NullableTextRowExpression[S, string] {
	return nullableTextRow(ReplaceNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(other)), AsSelected(rowInput(replacement))))
}

// ReplaceNullableValue applies ReplaceNullable to selected expressions, including aggregates/windows.
//
// Example: expr.ReplaceNullableValue(input, other, replacement)
func ReplaceNullableValue[S any, V, W, X ~string](input Selected[S, value.Nullable[V]], other Selected[S, value.Nullable[W]], replacement Selected[S, value.Nullable[X]]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.ReplaceOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input), operationArg(other), operationArg(replacement))
}

// Length counts Unicode characters rather than encoded bytes.
//
// Example: expr.Length(input)
func Length[S any, V ~string](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(LengthValue(AsSelected(rowInput(input))))
}

// LengthValue applies Length to selected expressions, including aggregates/windows.
//
// Example: expr.LengthValue(input)
func LengthValue[S any, V ~string](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.LengthOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// LengthNullable counts Unicode characters rather than encoded bytes, propagating SQL NULL.
//
// Example: expr.LengthNullable(input)
func LengthNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(LengthNullableValue(AsSelected(rowInput(input))))
}

// LengthNullableValue applies LengthNullable to selected expressions, including aggregates/windows.
//
// Example: expr.LengthNullableValue(input)
func LengthNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.LengthOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// OctetLength counts encoded bytes.
//
// Example: expr.OctetLength(input)
func OctetLength[S any, V ~string](input RowValue[S, V]) OrderedRowExpression[S, int64] {
	return orderedRow(OctetLengthValue(AsSelected(rowInput(input))))
}

// OctetLengthValue applies OctetLength to selected expressions, including aggregates/windows.
//
// Example: expr.OctetLengthValue(input)
func OctetLengthValue[S any, V ~string](input Selected[S, V]) Selected[S, int64] {
	return operationValue[S](querycore.OctetLengthOperation, columnCodec(codec.Signed[int64]()), operationArg(input))
}

// OctetLengthNullable counts encoded bytes, propagating SQL NULL.
//
// Example: expr.OctetLengthNullable(input)
func OctetLengthNullable[S any, V ~string](input RowValue[S, value.Nullable[V]]) NullableOrderedRowExpression[S, int64] {
	return nullableOrderedRow(OctetLengthNullableValue(AsSelected(rowInput(input))))
}

// OctetLengthNullableValue applies OctetLengthNullable to selected expressions, including aggregates/windows.
//
// Example: expr.OctetLengthNullableValue(input)
func OctetLengthNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[int64]] {
	return operationValue[S](querycore.OctetLengthOperation, columnCodec(codec.Nullable(codec.Signed[int64]())), operationArg(input))
}

// ConcatWS joins text with a bound, non-null separator. SQL NULL inputs are
// skipped; empty strings are retained. The result is always a non-null string.
//
// Example: expr.ConcatWS(separator, first, rest...)
func ConcatWS[S any, V ~string](separator string, first RowValue[S, V], rest ...RowValue[S, V]) TextRowExpression[S, string] {
	items := make([]Selected[S, V], len(rest))
	for i, v := range rest {
		items[i] = AsSelected(rowInput(v))
	}
	return textRow(ConcatWSValue(separator, AsSelected(rowInput(first)), items...))
}

// ConcatWSValue accepts selected text values and returns a non-null string.
//
// Example: expr.ConcatWSValue(separator, first, rest...)
func ConcatWSValue[S any, V ~string](separator string, first Selected[S, V], rest ...Selected[S, V]) Selected[S, string] {
	args := make([]ormcore.OperationArgument, 0, len(rest)+2)
	args = append(args, textOperationArgument(separator, nil), operationArg(first))
	for _, v := range rest {
		args = append(args, operationArg(v))
	}
	return operationValue[S](querycore.ConcatWSOperation, columnCodec(codec.String[string]()), args...)
}

// ConcatWSNullable joins text with a bound, non-null separator. SQL NULL inputs are
// skipped; empty strings are retained. The result is always a non-null string.
//
// Example: expr.ConcatWSNullable(separator, first, rest...)
func ConcatWSNullable[S any, V ~string](separator string, first RowValue[S, value.Nullable[V]], rest ...RowValue[S, value.Nullable[V]]) TextRowExpression[S, string] {
	items := make([]Selected[S, value.Nullable[V]], len(rest))
	for i, v := range rest {
		items[i] = AsSelected(rowInput(v))
	}
	return textRow(ConcatWSNullableValue(separator, AsSelected(rowInput(first)), items...))
}

// ConcatWSNullableValue accepts selected text values and returns a non-null string.
//
// Example: expr.ConcatWSNullableValue(separator, first, rest...)
func ConcatWSNullableValue[S any, V ~string](separator string, first Selected[S, value.Nullable[V]], rest ...Selected[S, value.Nullable[V]]) Selected[S, string] {
	args := make([]ormcore.OperationArgument, 0, len(rest)+2)
	args = append(args, textOperationArgument(separator, nil), operationArg(first))
	for _, v := range rest {
		args = append(args, operationArg(v))
	}
	return operationValue[S](querycore.ConcatWSOperation, columnCodec(codec.String[string]()), args...)
}

// Substring selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. A negative count is rejected before execution.
//
// Example: expr.Substring(input, start, count)
func Substring[S any, V ~string](input RowValue[S, V], start int32, count int32) TextRowExpression[S, string] {
	return textRow(SubstringValue(AsSelected(rowInput(input)), start, count))
}

// SubstringValue accepts a selected text value and bound character positions.
//
// Example: expr.SubstringValue(input, start, count)
func SubstringValue[S any, V ~string](input Selected[S, V], start int32, count int32) Selected[S, string] {
	args := []ormcore.OperationArgument{operationArg(input), integerArgument(start)}
	args = append(args, integerArgument(count))
	result := operationValue[S](querycore.SubstringOperation, columnCodec(codec.String[string]()), args...)
	if err := querycore.ValidateSubstringCount(count); err != nil {
		result.data.core.Err = safeerror.Join("operation failed", result.data.core.Err, err)
	}
	return result
}

// SubstringNullable selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. A negative count is rejected before execution.
//
// Example: expr.SubstringNullable(input, start, count)
func SubstringNullable[S any, V ~string](input RowValue[S, value.Nullable[V]], start int32, count int32) NullableTextRowExpression[S, string] {
	return nullableTextRow(SubstringNullableValue(AsSelected(rowInput(input)), start, count))
}

// SubstringNullableValue accepts a selected text value and bound character positions.
//
// Example: expr.SubstringNullableValue(input, start, count)
func SubstringNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]], start int32, count int32) Selected[S, value.Nullable[string]] {
	args := []ormcore.OperationArgument{operationArg(input), integerArgument(start)}
	args = append(args, integerArgument(count))
	result := operationValue[S](querycore.SubstringOperation, columnCodec(codec.Nullable(codec.String[string]())), args...)
	if err := querycore.ValidateSubstringCount(count); err != nil {
		result.data.core.Err = safeerror.Join("operation failed", result.data.core.Err, err)
	}
	return result
}

// SubstringFrom selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. It selects through the end.
//
// Example: expr.SubstringFrom(input, start)
func SubstringFrom[S any, V ~string](input RowValue[S, V], start int32) TextRowExpression[S, string] {
	return textRow(SubstringFromValue(AsSelected(rowInput(input)), start))
}

// SubstringFromValue accepts a selected text value and bound character positions.
//
// Example: expr.SubstringFromValue(input, start)
func SubstringFromValue[S any, V ~string](input Selected[S, V], start int32) Selected[S, string] {
	args := []ormcore.OperationArgument{operationArg(input), integerArgument(start)}

	result := operationValue[S](querycore.SubstringOperation, columnCodec(codec.String[string]()), args...)

	return result
}

// SubstringFromNullable selects characters using PostgreSQL's one-based positions.
// Zero/negative starts follow SQL semantics. It selects through the end.
//
// Example: expr.SubstringFromNullable(input, start)
func SubstringFromNullable[S any, V ~string](input RowValue[S, value.Nullable[V]], start int32) NullableTextRowExpression[S, string] {
	return nullableTextRow(SubstringFromNullableValue(AsSelected(rowInput(input)), start))
}

// SubstringFromNullableValue accepts a selected text value and bound character positions.
//
// Example: expr.SubstringFromNullableValue(input, start)
func SubstringFromNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]], start int32) Selected[S, value.Nullable[string]] {
	args := []ormcore.OperationArgument{operationArg(input), integerArgument(start)}

	result := operationValue[S](querycore.SubstringOperation, columnCodec(codec.Nullable(codec.String[string]())), args...)

	return result
}

// SubstringAt uses typed integer expressions as positions. Database negative-count
// and SQL-integer overflow errors remain runtime errors. Positions are non-null.
//
// Example: expr.SubstringAt(input, start, count)
func SubstringAt[S any, V ~string, I integerNumber](input RowValue[S, V], start, count RowValue[S, I]) TextRowExpression[S, string] {
	return textRow(SubstringAtValue(AsSelected(rowInput(input)), AsSelected(rowInput(start)), AsSelected(rowInput(count))))
}

// SubstringAtValue retains selected text/integer scopes for dynamic positions.
//
// Example: expr.SubstringAtValue(input, start, count)
func SubstringAtValue[S any, V ~string, I integerNumber](input Selected[S, V], start, count Selected[S, I]) Selected[S, string] {
	return operationValue[S](querycore.SubstringOperation, columnCodec(codec.String[string]()), operationArg(input), operationArg(start), operationArg(count))
}

// SubstringAtNullable uses typed integer expressions as positions. Database negative-count
// and SQL-integer overflow errors remain runtime errors. Positions are non-null.
//
// Example: expr.SubstringAtNullable(input, start, count)
func SubstringAtNullable[S any, V ~string, I integerNumber](input RowValue[S, value.Nullable[V]], start, count RowValue[S, I]) NullableTextRowExpression[S, string] {
	return nullableTextRow(SubstringAtNullableValue(AsSelected(rowInput(input)), AsSelected(rowInput(start)), AsSelected(rowInput(count))))
}

// SubstringAtNullableValue retains selected text/integer scopes for dynamic positions.
//
// Example: expr.SubstringAtNullableValue(input, start, count)
func SubstringAtNullableValue[S any, V ~string, I integerNumber](input Selected[S, value.Nullable[V]], start, count Selected[S, I]) Selected[S, value.Nullable[string]] {
	return operationValue[S](querycore.SubstringOperation, columnCodec(codec.Nullable(codec.String[string]())), operationArg(input), operationArg(start), operationArg(count))
}
