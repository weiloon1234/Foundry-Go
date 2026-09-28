package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func intervalOperationSpec(op scalarOperation) (operationSpec, bool) {
	switch op {
	case addIntervalsOperation:
		return operationSpec{"+", 2, 2, false}, true
	case subtractIntervalsOperation, localDifferenceOperation, instantDifferenceOperation, clockDifferenceOperation:
		return operationSpec{"-", 2, 2, false}, true
	case negateIntervalOperation:
		return operationSpec{"-", 1, 1, false}, true
	case intervalMonthsOperation, intervalDaysOperation, intervalElapsedOperation:
		return operationSpec{"EXTRACT", 1, 1, false}, true
	default:
		return operationSpec{}, false
	}
}
func (n operationNode) validateInterval() error {
	wantInput, wantResult := codec.TypeInterval, codec.TypeInterval
	switch n.kind {
	case localDifferenceOperation:
		wantInput = codec.TypeLocalDateTime
	case instantDifferenceOperation:
		wantInput = codec.TypeDateTime
	case clockDifferenceOperation:
		wantInput = codec.TypeTime
	case intervalMonthsOperation, intervalDaysOperation, intervalElapsedOperation:
		wantResult = codec.TypeInteger
	case addIntervalsOperation, subtractIntervalsOperation, negateIntervalOperation:
	default:
		return fault.New(fault.Invalid, "invalid interval operation")
	}
	if n.result != wantResult {
		return fault.New(fault.Invalid, "interval operation has an incompatible result")
	}
	for _, arg := range n.arguments {
		if arg.kind != wantInput {
			return fault.New(fault.Invalid, "interval operation has incompatible inputs")
		}
	}
	return nil
}
func intervalOperationSQL(n operationNode, args []string) (string, error) {
	spec, _ := n.kind.spec()
	switch n.kind {
	case addIntervalsOperation, subtractIntervalsOperation, localDifferenceOperation, instantDifferenceOperation, clockDifferenceOperation:
		return "(" + args[0] + " " + spec.name + " " + args[1] + ")", nil
	case negateIntervalOperation:
		return "(-" + args[0] + ")", nil
	case intervalMonthsOperation:
		return "CAST((EXTRACT(year FROM " + args[0] + ") * 12 + EXTRACT(month FROM " + args[0] + ")) AS bigint)", nil
	case intervalDaysOperation:
		return "CAST(EXTRACT(day FROM " + args[0] + ") AS bigint)", nil
	case intervalElapsedOperation:
		return "CAST((EXTRACT(hour FROM " + args[0] + ") * 3600000000000 + EXTRACT(minute FROM " + args[0] + ") * 60000000000 + EXTRACT(second FROM " + args[0] + ") * 1000000000) AS bigint)", nil
	default:
		return "", fault.New(fault.Invalid, "invalid interval operation")
	}
}
