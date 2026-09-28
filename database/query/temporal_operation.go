package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func temporalOperationSpec(op scalarOperation) (operationSpec, bool) {
	switch op {
	case extractYearOperation:
		return operationSpec{"year", 1, 1, false}, true
	case extractMonthOperation:
		return operationSpec{"month", 1, 1, false}, true
	case extractDayOperation:
		return operationSpec{"day", 1, 1, false}, true
	case extractDayOfWeekOperation:
		return operationSpec{"dow", 1, 1, false}, true
	case extractISODayOfWeekOperation:
		return operationSpec{"isodow", 1, 1, false}, true
	case extractDayOfYearOperation:
		return operationSpec{"doy", 1, 1, false}, true
	case extractISOWeekOperation:
		return operationSpec{"week", 1, 1, false}, true
	case extractISOYearOperation:
		return operationSpec{"isoyear", 1, 1, false}, true
	case extractQuarterOperation:
		return operationSpec{"quarter", 1, 1, false}, true
	case extractCenturyOperation:
		return operationSpec{"century", 1, 1, false}, true
	case extractDecadeOperation:
		return operationSpec{"decade", 1, 1, false}, true
	case extractMillenniumOperation:
		return operationSpec{"millennium", 1, 1, false}, true
	case extractJulianDayOperation:
		return operationSpec{"julian", 1, 1, false}, true
	case extractHourOperation:
		return operationSpec{"hour", 1, 1, false}, true
	case extractMinuteOperation:
		return operationSpec{"minute", 1, 1, false}, true
	case extractSecondOperation:
		return operationSpec{"second", 1, 1, false}, true
	case extractMillisecondsOperation:
		return operationSpec{"milliseconds", 1, 1, false}, true
	case extractMicrosecondsOperation:
		return operationSpec{"microseconds", 1, 1, false}, true
	case extractUnixSecondsOperation:
		return operationSpec{"epoch", 1, 1, false}, true
	case extractUnixMillisecondsOperation:
		return operationSpec{"epoch", 1, 1, false}, true
	case fromUnixMillisOperation:
		return operationSpec{"DATE_ADD", 1, 1, false}, true
	case truncateDateOperation:
		return operationSpec{"DATE_TRUNC", 2, 2, false}, true
	case truncateLocalOperation:
		return operationSpec{"DATE_TRUNC", 2, 2, false}, true
	case truncateInstantOperation:
		return operationSpec{"DATE_TRUNC", 3, 3, false}, true
	case localAtOperation:
		return operationSpec{"TIMEZONE", 2, 2, false}, true
	case resolveLocalOperation:
		return operationSpec{"TIMEZONE", 2, 2, false}, true
	case dateOfOperation:
		return operationSpec{"", 1, 1, false}, true
	case timeOfOperation:
		return operationSpec{"", 1, 1, false}, true
	case combineDateTimeOperation:
		return operationSpec{"+", 2, 2, false}, true
	case addDateDaysOperation:
		return operationSpec{"+", 2, 2, false}, true
	case dateDifferenceOperation:
		return operationSpec{"-", 2, 2, false}, true
	case addDateIntervalOperation:
		return operationSpec{"+", 2, 2, false}, true
	case subtractDateIntervalOperation:
		return operationSpec{"-", 2, 2, false}, true
	case addLocalIntervalOperation:
		return operationSpec{"+", 2, 2, false}, true
	case subtractLocalIntervalOperation:
		return operationSpec{"-", 2, 2, false}, true
	case addInstantIntervalOperation:
		return operationSpec{"DATE_ADD", 3, 3, false}, true
	case subtractInstantIntervalOperation:
		return operationSpec{"DATE_SUBTRACT", 3, 3, false}, true
	case addClockElapsedOperation:
		return operationSpec{"+", 2, 2, false}, true
	case subtractClockElapsedOperation:
		return operationSpec{"-", 2, 2, false}, true
	case transactionTimeOperation:
		return operationSpec{"TRANSACTION_TIMESTAMP", 0, 0, false}, true
	default:
		return intervalOperationSpec(op)
	}
}
func (n operationNode) validateTemporal() error {
	if n.kind >= addIntervalsOperation {
		return n.validateInterval()
	}
	same := func(kinds ...codec.ParameterType) bool {
		if len(kinds) != len(n.arguments) {
			return false
		}
		for i, k := range kinds {
			if n.arguments[i].kind != k {
				return false
			}
		}
		return true
	}
	valid := false
	var result codec.ParameterType
	switch {
	case n.kind >= extractYearOperation && n.kind <= extractJulianDayOperation:
		valid = same(codec.TypeDate) || same(codec.TypeLocalDateTime)
		result = codec.TypeInteger
		if n.kind == extractJulianDayOperation {
			result = codec.TypeDecimal
		}
	case n.kind >= extractHourOperation && n.kind <= extractMicrosecondsOperation:
		valid = same(codec.TypeTime) || same(codec.TypeLocalDateTime)
		result = codec.TypeInteger
		if n.kind == extractSecondOperation || n.kind == extractMillisecondsOperation {
			result = codec.TypeDecimal
		}
	case n.kind == extractUnixSecondsOperation || n.kind == extractUnixMillisecondsOperation:
		valid = same(codec.TypeDateTime)
		result = codec.TypeDecimal
		if n.kind == extractUnixMillisecondsOperation {
			result = codec.TypeInteger
		}
	default:
		switch n.kind {
		case fromUnixMillisOperation:
			valid, result = same(codec.TypeInteger), codec.TypeDateTime
		case truncateDateOperation:
			valid, result = same(codec.TypeText, codec.TypeDate), codec.TypeDate
		case truncateLocalOperation:
			valid, result = same(codec.TypeText, codec.TypeLocalDateTime), codec.TypeLocalDateTime
		case truncateInstantOperation:
			valid, result = same(codec.TypeText, codec.TypeDateTime, codec.TypeText), codec.TypeDateTime
		case localAtOperation:
			valid, result = same(codec.TypeText, codec.TypeDateTime), codec.TypeLocalDateTime
		case resolveLocalOperation:
			valid, result = same(codec.TypeText, codec.TypeLocalDateTime), codec.TypeDateTime
		case dateOfOperation:
			valid, result = same(codec.TypeLocalDateTime), codec.TypeDate
		case timeOfOperation:
			valid, result = same(codec.TypeLocalDateTime), codec.TypeTime
		case combineDateTimeOperation:
			valid, result = same(codec.TypeDate, codec.TypeTime), codec.TypeLocalDateTime
		case addDateDaysOperation:
			valid, result = same(codec.TypeDate, codec.TypeInteger), codec.TypeDate
		case dateDifferenceOperation:
			valid, result = same(codec.TypeDate, codec.TypeDate), codec.TypeInteger
		case addDateIntervalOperation, subtractDateIntervalOperation:
			valid, result = same(codec.TypeDate, codec.TypeInterval), codec.TypeLocalDateTime
		case addLocalIntervalOperation, subtractLocalIntervalOperation:
			valid, result = same(codec.TypeLocalDateTime, codec.TypeInterval), codec.TypeLocalDateTime
		case addInstantIntervalOperation, subtractInstantIntervalOperation:
			valid, result = same(codec.TypeDateTime, codec.TypeInterval, codec.TypeText), codec.TypeDateTime
		case addClockElapsedOperation, subtractClockElapsedOperation:
			valid, result = same(codec.TypeTime, codec.TypeInterval), codec.TypeTime
		case transactionTimeOperation:
			valid, result = same(), codec.TypeDateTime
		}
	}
	if !valid || n.result != result {
		return fault.New(fault.Invalid, "temporal operation has incompatible inputs or result")
	}
	return nil
}

func temporalOperationSQL(n operationNode, args []string) (string, error) {
	spec, _ := n.kind.spec()
	if n.kind >= extractYearOperation && n.kind <= extractUnixMillisecondsOperation {
		text := "EXTRACT(" + spec.name + " FROM " + args[0] + ")"
		if n.kind == extractUnixMillisecondsOperation {
			text = "FLOOR(" + text + " * 1000)"
		}
		if n.result == codec.TypeInteger {
			text = "CAST(" + text + " AS bigint)"
		}
		return text, nil
	}
	switch n.kind {
	case truncateDateOperation:
		return "CAST(DATE_TRUNC(" + args[0] + ", CAST(" + args[1] + " AS timestamp without time zone)) AS date)", nil
	case truncateLocalOperation, localAtOperation, resolveLocalOperation:
		return spec.name + "(" + args[0] + ", " + args[1] + ")", nil
	case truncateInstantOperation:
		return spec.name + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", nil
	case fromUnixMillisOperation:
		// PostgreSQL parses integral milliseconds with checked integer scaling.
		// Keep one occurrence of the input so nested conversions grow linearly
		// and preserve aggregate/window evaluation at this SELECT level.
		return spec.name + "(TIMESTAMPTZ '1970-01-01 00:00:00+00', CAST((CAST(" + args[0] + " AS text) || ' milliseconds') AS interval), 'UTC')", nil
	case dateOfOperation, timeOfOperation:
		kind, _ := parameterSQLType(n.result)
		return "CAST(" + args[0] + " AS " + kind + ")", nil
	case combineDateTimeOperation:
		return "(" + args[0] + " + " + args[1] + ")", nil
	case addDateDaysOperation:
		return "(" + args[0] + " + CAST(" + args[1] + " AS integer))", nil
	case dateDifferenceOperation:
		return "CAST((" + args[0] + " - " + args[1] + ") AS bigint)", nil
	case addDateIntervalOperation, subtractDateIntervalOperation, addLocalIntervalOperation, subtractLocalIntervalOperation, addClockElapsedOperation, subtractClockElapsedOperation:
		return "(" + args[0] + " " + spec.name + " " + args[1] + ")", nil
	case addInstantIntervalOperation, subtractInstantIntervalOperation:
		return spec.name + "(" + args[0] + ", " + args[1] + ", " + args[2] + ")", nil
	case transactionTimeOperation:
		return spec.name + "()", nil
	default:
		return intervalOperationSQL(n, args)
	}
}
