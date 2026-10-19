package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TemporalOperationSpec(op ScalarOperation) (OperationSpec, bool) {
	switch op {
	case ExtractYearOperation:
		return OperationSpec{"year", 1, 1, false}, true
	case ExtractMonthOperation:
		return OperationSpec{"month", 1, 1, false}, true
	case ExtractDayOperation:
		return OperationSpec{"day", 1, 1, false}, true
	case ExtractDayOfWeekOperation:
		return OperationSpec{"dow", 1, 1, false}, true
	case ExtractISODayOfWeekOperation:
		return OperationSpec{"isodow", 1, 1, false}, true
	case ExtractDayOfYearOperation:
		return OperationSpec{"doy", 1, 1, false}, true
	case ExtractISOWeekOperation:
		return OperationSpec{"week", 1, 1, false}, true
	case ExtractISOYearOperation:
		return OperationSpec{"isoyear", 1, 1, false}, true
	case ExtractQuarterOperation:
		return OperationSpec{"quarter", 1, 1, false}, true
	case ExtractCenturyOperation:
		return OperationSpec{"century", 1, 1, false}, true
	case ExtractDecadeOperation:
		return OperationSpec{"decade", 1, 1, false}, true
	case ExtractMillenniumOperation:
		return OperationSpec{"millennium", 1, 1, false}, true
	case ExtractJulianDayOperation:
		return OperationSpec{"julian", 1, 1, false}, true
	case ExtractHourOperation:
		return OperationSpec{"hour", 1, 1, false}, true
	case ExtractMinuteOperation:
		return OperationSpec{"minute", 1, 1, false}, true
	case ExtractSecondOperation:
		return OperationSpec{"second", 1, 1, false}, true
	case ExtractMillisecondsOperation:
		return OperationSpec{"milliseconds", 1, 1, false}, true
	case ExtractMicrosecondsOperation:
		return OperationSpec{"microseconds", 1, 1, false}, true
	case ExtractUnixSecondsOperation:
		return OperationSpec{"epoch", 1, 1, false}, true
	case ExtractUnixMillisecondsOperation:
		return OperationSpec{"epoch", 1, 1, false}, true
	case FromUnixMillisOperation:
		return OperationSpec{"DATE_ADD", 1, 1, false}, true
	case TruncateDateOperation:
		return OperationSpec{"DATE_TRUNC", 2, 2, false}, true
	case TruncateLocalOperation:
		return OperationSpec{"DATE_TRUNC", 2, 2, false}, true
	case TruncateInstantOperation:
		return OperationSpec{"DATE_TRUNC", 3, 3, false}, true
	case LocalAtOperation:
		return OperationSpec{"TIMEZONE", 2, 2, false}, true
	case ResolveLocalOperation:
		return OperationSpec{"TIMEZONE", 2, 2, false}, true
	case DateOfOperation:
		return OperationSpec{"", 1, 1, false}, true
	case TimeOfOperation:
		return OperationSpec{"", 1, 1, false}, true
	case CombineDateTimeOperation:
		return OperationSpec{"+", 2, 2, false}, true
	case AddDateDaysOperation:
		return OperationSpec{"+", 2, 2, false}, true
	case DateDifferenceOperation:
		return OperationSpec{"-", 2, 2, false}, true
	case AddDateIntervalOperation:
		return OperationSpec{"+", 2, 2, false}, true
	case SubtractDateIntervalOperation:
		return OperationSpec{"-", 2, 2, false}, true
	case AddLocalIntervalOperation:
		return OperationSpec{"+", 2, 2, false}, true
	case SubtractLocalIntervalOperation:
		return OperationSpec{"-", 2, 2, false}, true
	case AddInstantIntervalOperation:
		return OperationSpec{"DATE_ADD", 3, 3, false}, true
	case SubtractInstantIntervalOperation:
		return OperationSpec{"DATE_SUBTRACT", 3, 3, false}, true
	case AddClockElapsedOperation:
		return OperationSpec{"+", 2, 2, false}, true
	case SubtractClockElapsedOperation:
		return OperationSpec{"-", 2, 2, false}, true
	case TransactionTimeOperation:
		return OperationSpec{"TRANSACTION_TIMESTAMP", 0, 0, false}, true
	default:
		return IntervalOperationSpec(op)
	}
}

func (n OperationNode) ValidateTemporal() error {
	if n.Kind >= AddIntervalsOperation {
		return n.ValidateInterval()
	}
	same := func(kinds ...codec.ParameterType) bool {
		if len(kinds) != len(n.Arguments) {
			return false
		}
		for i, k := range kinds {
			if n.Arguments[i].Kind != k {
				return false
			}
		}
		return true
	}
	valid := false
	var result codec.ParameterType
	switch {
	case n.Kind >= ExtractYearOperation && n.Kind <= ExtractJulianDayOperation:
		valid = same(codec.TypeDate) || same(codec.TypeLocalDateTime)
		result = codec.TypeInteger
		if n.Kind == ExtractJulianDayOperation {
			result = codec.TypeDecimal
		}
	case n.Kind >= ExtractHourOperation && n.Kind <= ExtractMicrosecondsOperation:
		valid = same(codec.TypeTime) || same(codec.TypeLocalDateTime)
		result = codec.TypeInteger
		if n.Kind == ExtractSecondOperation || n.Kind == ExtractMillisecondsOperation {
			result = codec.TypeDecimal
		}
	case n.Kind == ExtractUnixSecondsOperation || n.Kind == ExtractUnixMillisecondsOperation:
		valid = same(codec.TypeDateTime)
		result = codec.TypeDecimal
		if n.Kind == ExtractUnixMillisecondsOperation {
			result = codec.TypeInteger
		}
	default:
		switch n.Kind {
		case FromUnixMillisOperation:
			valid, result = same(codec.TypeInteger), codec.TypeDateTime
		case TruncateDateOperation:
			valid, result = same(codec.TypeText, codec.TypeDate), codec.TypeDate
		case TruncateLocalOperation:
			valid, result = same(codec.TypeText, codec.TypeLocalDateTime), codec.TypeLocalDateTime
		case TruncateInstantOperation:
			valid, result = same(codec.TypeText, codec.TypeDateTime, codec.TypeText), codec.TypeDateTime
		case LocalAtOperation:
			valid, result = same(codec.TypeText, codec.TypeDateTime), codec.TypeLocalDateTime
		case ResolveLocalOperation:
			valid, result = same(codec.TypeText, codec.TypeLocalDateTime), codec.TypeDateTime
		case DateOfOperation:
			valid, result = same(codec.TypeLocalDateTime), codec.TypeDate
		case TimeOfOperation:
			valid, result = same(codec.TypeLocalDateTime), codec.TypeTime
		case CombineDateTimeOperation:
			valid, result = same(codec.TypeDate, codec.TypeTime), codec.TypeLocalDateTime
		case AddDateDaysOperation:
			valid, result = same(codec.TypeDate, codec.TypeInteger), codec.TypeDate
		case DateDifferenceOperation:
			valid, result = same(codec.TypeDate, codec.TypeDate), codec.TypeInteger
		case AddDateIntervalOperation, SubtractDateIntervalOperation:
			valid, result = same(codec.TypeDate, codec.TypeInterval), codec.TypeLocalDateTime
		case AddLocalIntervalOperation, SubtractLocalIntervalOperation:
			valid, result = same(codec.TypeLocalDateTime, codec.TypeInterval), codec.TypeLocalDateTime
		case AddInstantIntervalOperation, SubtractInstantIntervalOperation:
			valid, result = same(codec.TypeDateTime, codec.TypeInterval, codec.TypeText), codec.TypeDateTime
		case AddClockElapsedOperation, SubtractClockElapsedOperation:
			valid, result = same(codec.TypeTime, codec.TypeInterval), codec.TypeTime
		case TransactionTimeOperation:
			valid, result = same(), codec.TypeDateTime
		}
	}
	if !valid || n.Result != result {
		return fault.New(fault.Invalid, "temporal operation has incompatible inputs or result")
	}
	return nil
}

func TemporalOperationSQL(n OperationNode, args []string) (string, error) {
	spec, _ := n.Kind.Spec()
	if n.Kind >= ExtractYearOperation && n.Kind <= ExtractUnixMillisecondsOperation {
		text := "EXTRACT(" + spec.Name + " FROM " + args[0] + ")"
		if n.Kind == ExtractUnixMillisecondsOperation {
			text = "FLOOR(" + text + " * 1000)"
		}
		if n.Result == codec.TypeInteger {
			text = "CAST(" + text + " AS bigint)"
		}
		return text, nil
	}
	switch n.Kind {
	case TruncateDateOperation:
		return "CAST(DATE_TRUNC(" + args[0] + ", CAST(" + args[1] + " AS timestamp without time zone)) AS date)", nil
	case TruncateLocalOperation, LocalAtOperation, ResolveLocalOperation:
		return spec.Name + "(" + args[0] + ", " + args[1] + ")", nil
	case TruncateInstantOperation:
		return spec.Name + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", nil
	case FromUnixMillisOperation:
		// PostgreSQL parses integral milliseconds with checked integer scaling.
		// Keep one occurrence of the input so nested conversions grow linearly
		// and preserve aggregate/window evaluation at this SELECT level.
		return spec.Name + "(TIMESTAMPTZ '1970-01-01 00:00:00+00', CAST((CAST(" + args[0] + " AS text) || ' milliseconds') AS interval), 'UTC')", nil
	case DateOfOperation, TimeOfOperation:
		kind, _ := ParameterSQLType(n.Result)
		return "CAST(" + args[0] + " AS " + kind + ")", nil
	case CombineDateTimeOperation:
		return "(" + args[0] + " + " + args[1] + ")", nil
	case AddDateDaysOperation:
		return "(" + args[0] + " + CAST(" + args[1] + " AS integer))", nil
	case DateDifferenceOperation:
		return "CAST((" + args[0] + " - " + args[1] + ") AS bigint)", nil
	case AddDateIntervalOperation, SubtractDateIntervalOperation, AddLocalIntervalOperation, SubtractLocalIntervalOperation, AddClockElapsedOperation, SubtractClockElapsedOperation:
		return "(" + args[0] + " " + spec.Name + " " + args[1] + ")", nil
	case AddInstantIntervalOperation, SubtractInstantIntervalOperation:
		return spec.Name + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", nil
	case TransactionTimeOperation:
		return spec.Name + "()", nil
	default:
		return IntervalOperationSQL(n, args)
	}
}
