package expr

import (
	"context"
	"math"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TrigramMatch compares typed text with pg_trgm's indexable public.% operator.
// It honours the transaction's similarity threshold (PostgreSQL default 0.3).
// The application must install pg_trgm in public and declare its trigram index.
//
// Example: expr.TrigramMatch(expr.Value(orm.TextExpr(Names.Normalized)), search)
func TrigramMatch[S any, V ~string](input RowValue[S, V], search V) Predicate[S] {
	return Predicate[S]{core: rowCompare(checkedTextColumn(rowInput(input).data), querycore.TrigramMatch, search)}
}

// TrigramSimilarity returns public.similarity as float64, retaining the row owner.
// Use TrigramMatch alongside a score comparison to allow a trigram index scan.
//
// Example: expr.TrigramSimilarity(expr.Value(orm.TextExpr(Names.Normalized)), search)
func TrigramSimilarity[S any, V ~string](input RowValue[S, V], search V) OrderedRowExpression[S, float64] {
	return orderedRow(TrigramSimilarityValue(AsSelected(rowInput(input)), search))
}

// TrigramSimilarityValue preserves selected phase, including aggregates/windows.
//
// Example: expr.TrigramSimilarityValue(selectedName, search)
func TrigramSimilarityValue[S any, V ~string](input Selected[S, V], search V) Selected[S, float64] {
	return operationValue[S](querycore.TrigramSimilarityOperation, columnCodec(codec.Float[float64]()), operationArg(input), operationArg(AsSelected(ParamOf(input, search))))
}

// TrigramSimilarityNullable propagates SQL NULL from the text operand.
//
// Example: expr.TrigramSimilarityNullable(expr.Value(orm.NullableTextExpr(Reviews.Body)), search)
func TrigramSimilarityNullable[S any, V ~string](input RowValue[S, value.Nullable[V]], search V) NullableOrderedRowExpression[S, float64] {
	return nullableOrderedRow(TrigramSimilarityNullableValue(AsSelected(rowInput(input)), search))
}

// TrigramSimilarityNullableValue is the selected nullable counterpart.
//
// Example: expr.TrigramSimilarityNullableValue(selectedName, search)
func TrigramSimilarityNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]], search V) Selected[S, value.Nullable[float64]] {
	return operationValue[S](querycore.TrigramSimilarityOperation, columnCodec(codec.Nullable(codec.Float[float64]())), operationArg(input), operationArg(AsSelected(ParamOf(input, value.Of(search)))))
}

// SetTrigramThreshold sets pg_trgm.similarity_threshold for the current transaction.
// It rejects nil/inactive transactions, non-finite values, values outside [0, 1]
// and positive subnormal float64 values before I/O. Negative zero becomes zero.
// PostgreSQL restores the previous setting at transaction end.
//
// Example: expr.SetTrigramThreshold(ctx, tx, 0.6)
func SetTrigramThreshold(ctx context.Context, tx *database.Tx, threshold float64) error {
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 1 {
		return safeerror.NewFault(fault.Invalid, "trigram threshold must be finite and between zero and one")
	}
	// PostgreSQL's real GUC parser uses strtod and rejects underflow (ERANGE).
	const smallestNormal = 0x1p-1022
	if threshold > 0 && threshold < smallestNormal {
		return safeerror.NewFault(fault.Invalid, "trigram threshold is below PostgreSQL's normal floating range")
	}
	if threshold == 0 {
		threshold = 0
	}
	if tx == nil || tx.State() != database.TxActive {
		return safeerror.NewFault(fault.Invalid, "trigram threshold requires an active transaction")
	}
	_, err := tx.Exec(ctx, "SELECT set_config('pg_trgm.similarity_threshold', $1, true)", strconv.FormatFloat(threshold, 'g', -1, 64))
	return err
}
