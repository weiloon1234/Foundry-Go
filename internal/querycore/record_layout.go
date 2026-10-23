package querycore

import "github.com/weiloon1234/Foundry-Go/fault"

// RecordLayout describes the complete output expected by a record decoder.
type RecordLayout struct {
	Node       SelectNode
	Columns    []Column
	HasDecoder bool
	Err        error
}

// CompatibleRecords validates ordered output layouts for sets and recursive steps.
func CompatibleRecords(left, right RecordLayout) error {
	for _, r := range []RecordLayout{left, right} {
		if r.Err != nil {
			return r.Err
		}
		if len(r.Columns) == 0 || len(r.Columns) > MaxExpressionNodes || len(r.Columns) != len(r.Node.Selections) || !r.HasDecoder {
			return fault.New(fault.Invalid, "set input requires a complete record and decoder")
		}
	}
	if len(left.Columns) != len(right.Columns) {
		return fault.New(fault.Invalid, "set input column counts differ")
	}
	for i, l := range left.Columns {
		r := right.Columns[i]
		if l.Name != r.Name || l.Nullable != r.Nullable {
			return fault.New(fault.Invalid, "set input record layouts differ")
		}
	}
	return nil
}
