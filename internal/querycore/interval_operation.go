package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func IntervalOperationSpec(op ScalarOperation) (OperationSpec, bool) {
	switch op {
	case AddIntervalsOperation:
		return OperationSpec{"+", 2, 2, false}, true
	case SubtractIntervalsOperation, LocalDifferenceOperation, InstantDifferenceOperation, ClockDifferenceOperation:
		return OperationSpec{"-", 2, 2, false}, true
	case NegateIntervalOperation:
		return OperationSpec{"-", 1, 1, false}, true
	case IntervalMonthsOperation, IntervalDaysOperation, IntervalElapsedOperation:
		return OperationSpec{"EXTRACT", 1, 1, false}, true
	default:
		return OperationSpec{}, false
	}
}

func (n OperationNode) ValidateInterval() error {
	wantInput, wantResult := codec.TypeInterval, codec.TypeInterval
	switch n.Kind {
	case LocalDifferenceOperation:
		wantInput = codec.TypeLocalDateTime
	case InstantDifferenceOperation:
		wantInput = codec.TypeDateTime
	case ClockDifferenceOperation:
		wantInput = codec.TypeTime
	case IntervalMonthsOperation, IntervalDaysOperation, IntervalElapsedOperation:
		wantResult = codec.TypeInteger
	case AddIntervalsOperation, SubtractIntervalsOperation, NegateIntervalOperation:
	default:
		return safeerror.NewFault(fault.Invalid, "invalid interval operation")
	}
	if n.Result != wantResult {
		return safeerror.NewFault(fault.Invalid, "interval operation has an incompatible result")
	}
	for _, arg := range n.Arguments {
		if arg.Kind != wantInput {
			return safeerror.NewFault(fault.Invalid, "interval operation has incompatible inputs")
		}
	}
	return nil
}

func IntervalOperationSQL(n OperationNode, args []string) (string, error) {
	spec, _ := n.Kind.Spec()
	switch n.Kind {
	case AddIntervalsOperation, SubtractIntervalsOperation, LocalDifferenceOperation, InstantDifferenceOperation, ClockDifferenceOperation:
		return "(" + args[0] + " " + spec.Name + " " + args[1] + ")", nil
	case NegateIntervalOperation:
		return "(-" + args[0] + ")", nil
	case IntervalMonthsOperation:
		return "CAST((EXTRACT(year FROM " + args[0] + ") * 12 + EXTRACT(month FROM " + args[0] + ")) AS bigint)", nil
	case IntervalDaysOperation:
		return "CAST(EXTRACT(day FROM " + args[0] + ") AS bigint)", nil
	case IntervalElapsedOperation:
		return "CAST((EXTRACT(hour FROM " + args[0] + ") * 3600000000000 + EXTRACT(minute FROM " + args[0] + ") * 60000000000 + EXTRACT(second FROM " + args[0] + ") * 1000000000) AS bigint)", nil
	default:
		return "", safeerror.NewFault(fault.Invalid, "invalid interval operation")
	}
}
