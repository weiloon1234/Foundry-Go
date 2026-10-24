package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type scalarOperation = querycore.ScalarOperation

const addOperation = querycore.AddOperation

const subtractOperation = querycore.SubtractOperation

const multiplyOperation = querycore.MultiplyOperation

const divideOperation = querycore.DivideOperation

const remainderOperation = querycore.RemainderOperation

const negateOperation = querycore.NegateOperation

const absOperation = querycore.AbsOperation

const castOperation = querycore.CastOperation

const lowerOperation = querycore.LowerOperation

const upperOperation = querycore.UpperOperation

const trimOperation = querycore.TrimOperation

const trimLeftOperation = querycore.TrimLeftOperation

const trimRightOperation = querycore.TrimRightOperation

const lengthOperation = querycore.LengthOperation

const octetLengthOperation = querycore.OctetLengthOperation

const concatOperation = querycore.ConcatOperation

const concatWSOperation = querycore.ConcatWSOperation

const substringOperation = querycore.SubstringOperation

const replaceOperation = querycore.ReplaceOperation

const extractYearOperation = querycore.ExtractYearOperation

const extractMonthOperation = querycore.ExtractMonthOperation

const extractDayOperation = querycore.ExtractDayOperation

const extractDayOfWeekOperation = querycore.ExtractDayOfWeekOperation

const extractISODayOfWeekOperation = querycore.ExtractISODayOfWeekOperation

const extractDayOfYearOperation = querycore.ExtractDayOfYearOperation

const extractISOWeekOperation = querycore.ExtractISOWeekOperation

const extractISOYearOperation = querycore.ExtractISOYearOperation

const extractQuarterOperation = querycore.ExtractQuarterOperation

const extractCenturyOperation = querycore.ExtractCenturyOperation

const extractDecadeOperation = querycore.ExtractDecadeOperation

const extractMillenniumOperation = querycore.ExtractMillenniumOperation

const extractJulianDayOperation = querycore.ExtractJulianDayOperation

const extractHourOperation = querycore.ExtractHourOperation

const extractMinuteOperation = querycore.ExtractMinuteOperation

const extractSecondOperation = querycore.ExtractSecondOperation

const extractMillisecondsOperation = querycore.ExtractMillisecondsOperation

const extractMicrosecondsOperation = querycore.ExtractMicrosecondsOperation

const extractUnixSecondsOperation = querycore.ExtractUnixSecondsOperation

const extractUnixMillisecondsOperation = querycore.ExtractUnixMillisecondsOperation

const fromUnixMillisOperation = querycore.FromUnixMillisOperation

const truncateDateOperation = querycore.TruncateDateOperation

const truncateLocalOperation = querycore.TruncateLocalOperation

const truncateInstantOperation = querycore.TruncateInstantOperation

const localAtOperation = querycore.LocalAtOperation

const resolveLocalOperation = querycore.ResolveLocalOperation

const dateOfOperation = querycore.DateOfOperation

const timeOfOperation = querycore.TimeOfOperation

const combineDateTimeOperation = querycore.CombineDateTimeOperation

const addDateDaysOperation = querycore.AddDateDaysOperation

const dateDifferenceOperation = querycore.DateDifferenceOperation

const addDateIntervalOperation = querycore.AddDateIntervalOperation

const subtractDateIntervalOperation = querycore.SubtractDateIntervalOperation

const addLocalIntervalOperation = querycore.AddLocalIntervalOperation

const subtractLocalIntervalOperation = querycore.SubtractLocalIntervalOperation

const addInstantIntervalOperation = querycore.AddInstantIntervalOperation

const subtractInstantIntervalOperation = querycore.SubtractInstantIntervalOperation

const addClockElapsedOperation = querycore.AddClockElapsedOperation

const subtractClockElapsedOperation = querycore.SubtractClockElapsedOperation

const transactionTimeOperation = querycore.TransactionTimeOperation

const addIntervalsOperation = querycore.AddIntervalsOperation

const subtractIntervalsOperation = querycore.SubtractIntervalsOperation

const negateIntervalOperation = querycore.NegateIntervalOperation

const localDifferenceOperation = querycore.LocalDifferenceOperation

const instantDifferenceOperation = querycore.InstantDifferenceOperation

const clockDifferenceOperation = querycore.ClockDifferenceOperation

const intervalMonthsOperation = querycore.IntervalMonthsOperation

const intervalDaysOperation = querycore.IntervalDaysOperation

const intervalElapsedOperation = querycore.IntervalElapsedOperation

const jsonContainsOperation = querycore.JsonContainsOperation

const jsonContainedByOperation = querycore.JsonContainedByOperation

const jsonKindOperation = querycore.JsonKindOperation

const jsonPropertyOperation = querycore.JsonPropertyOperation

const jsonIndexOperation = querycore.JsonIndexOperation

const jsonScalarOperation = querycore.JsonScalarOperation

const jsonUnquoteOperation = querycore.JsonUnquoteOperation

const jsonArrayLengthOperation = querycore.JsonArrayLengthOperation

const sphericalDistanceOperation = querycore.SphericalDistanceOperation

type operationSpec = querycore.OperationSpec

type operationArgument = querycore.OperationArgument

type operationNode = querycore.OperationNode

func numericRepresentation(kind codec.ParameterType) bool {
	return querycore.NumericRepresentation(kind)
}

func operationArg[S, V any](e Expression[S, V]) operationArgument {
	return operationArgument{Value: querycore.WithSensitivity(e.node, e.codec.SensitiveValues()), Kind: e.codec.ParameterType()}
}

func operationValue[S, V any](op scalarOperation, result codec.Codec[V], arguments ...operationArgument) Expression[S, V] {
	return Expression[S, V]{node: operationNode{Kind: op, Arguments: slices.Clone(arguments), Result: result.ParameterType()}, codec: result}
}
