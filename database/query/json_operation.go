package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func jsonOperationSpec(op scalarOperation) (operationSpec, bool) {
	switch op {
	case jsonContainsOperation:
		return operationSpec{"@>", 2, 2, true}, true
	case jsonContainedByOperation:
		return operationSpec{"<@", 2, 2, true}, true
	case jsonKindOperation:
		return operationSpec{"JSONB_TYPEOF", 1, 1, false}, true
	case jsonPropertyOperation, jsonIndexOperation:
		return operationSpec{"->", 2, 2, true}, true
	case jsonScalarOperation, jsonUnquoteOperation:
		return operationSpec{"", 1, 1, false}, true
	case jsonArrayLengthOperation:
		return operationSpec{"JSONB_ARRAY_LENGTH", 1, 1, false}, true
	default:
		return operationSpec{}, false
	}
}

func (n operationNode) validateJSON() error {
	for i, input := range n.arguments {
		want := codec.TypeJSON
		if i == 1 && n.kind == jsonPropertyOperation {
			want = codec.TypeText
		}
		if i == 1 && n.kind == jsonIndexOperation {
			want = codec.TypeInteger
		}
		if input.kind != want {
			return fault.New(fault.Invalid, "JSON operation has incompatible inputs")
		}
	}
	want := codec.TypeBoolean
	if n.kind == jsonKindOperation {
		want = codec.TypeText
	}
	if n.kind == jsonPropertyOperation || n.kind == jsonIndexOperation || n.kind == jsonUnquoteOperation {
		want = codec.TypeJSON
	}
	if n.kind == jsonArrayLengthOperation {
		want = codec.TypeInteger
	}
	if n.kind == jsonScalarOperation {
		if n.result == codec.TypeJSON || n.result == codec.TypeBytes {
			return fault.New(fault.Invalid, "JSON scalar extraction requires a scalar codec")
		}
		return nil // The shared operation validator checks the closed SQL type.
	}
	if n.result != want {
		return fault.New(fault.Invalid, "JSON operation has an incompatible result")
	}
	return nil
}

func jsonOperationSQL(n operationNode, args []string) (string, error) {
	if n.kind == jsonScalarOperation {
		target, _ := parameterSQLType(n.result)
		return "CAST((" + args[0] + " #>> '{}') AS " + target + ")", nil
	}
	if n.kind == jsonArrayLengthOperation {
		// JSONB_ARRAY_LENGTH fails on non-arrays; any other kind is SQL NULL.
		return "CAST(CASE WHEN JSONB_TYPEOF(" + args[0] + ") = 'array' THEN JSONB_ARRAY_LENGTH(" + args[0] + ") END AS bigint)", nil
	}
	if n.kind == jsonUnquoteOperation {
		// Keep explicit JSON null when decoding a declared ,string property.
		return "CASE WHEN " + args[0] + " = 'null'::jsonb THEN 'null'::jsonb ELSE CAST((" + args[0] + " #>> '{}') AS jsonb) END", nil
	}
	spec, _ := n.kind.spec()
	if spec.infix {
		return "(" + args[0] + " " + spec.name + " " + args[1] + ")", nil
	}
	return spec.name + "(" + args[0] + ")", nil
}
