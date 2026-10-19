package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func JsonOperationSpec(op ScalarOperation) (OperationSpec, bool) {
	switch op {
	case JsonContainsOperation:
		return OperationSpec{"@>", 2, 2, true}, true
	case JsonContainedByOperation:
		return OperationSpec{"<@", 2, 2, true}, true
	case JsonKindOperation:
		return OperationSpec{"JSONB_TYPEOF", 1, 1, false}, true
	case JsonPropertyOperation, JsonIndexOperation:
		return OperationSpec{"->", 2, 2, true}, true
	case JsonScalarOperation, JsonUnquoteOperation:
		return OperationSpec{"", 1, 1, false}, true
	case JsonArrayLengthOperation:
		return OperationSpec{"JSONB_ARRAY_LENGTH", 1, 1, false}, true
	default:
		return OperationSpec{}, false
	}
}

func (n OperationNode) ValidateJSON() error {
	for i, input := range n.Arguments {
		want := codec.TypeJSON
		if i == 1 && n.Kind == JsonPropertyOperation {
			want = codec.TypeText
		}
		if i == 1 && n.Kind == JsonIndexOperation {
			want = codec.TypeInteger
		}
		if input.Kind != want {
			return fault.New(fault.Invalid, "JSON operation has incompatible inputs")
		}
	}
	want := codec.TypeBoolean
	if n.Kind == JsonKindOperation {
		want = codec.TypeText
	}
	if n.Kind == JsonPropertyOperation || n.Kind == JsonIndexOperation || n.Kind == JsonUnquoteOperation {
		want = codec.TypeJSON
	}
	if n.Kind == JsonArrayLengthOperation {
		want = codec.TypeInteger
	}
	if n.Kind == JsonScalarOperation {
		if n.Result == codec.TypeJSON || n.Result == codec.TypeBytes {
			return fault.New(fault.Invalid, "JSON scalar extraction requires a scalar codec")
		}
		return nil // The shared operation validator checks the closed SQL type.
	}
	if n.Result != want {
		return fault.New(fault.Invalid, "JSON operation has an incompatible result")
	}
	return nil
}

func JsonOperationSQL(n OperationNode, args []string) (string, error) {
	if n.Kind == JsonScalarOperation {
		target, _ := ParameterSQLType(n.Result)
		return "CAST((" + args[0] + " #>> '{}') AS " + target + ")", nil
	}
	if n.Kind == JsonArrayLengthOperation {
		// JSONB_ARRAY_LENGTH fails on non-arrays; any other kind is SQL NULL.
		return "CAST(CASE WHEN JSONB_TYPEOF(" + args[0] + ") = 'array' THEN JSONB_ARRAY_LENGTH(" + args[0] + ") END AS bigint)", nil
	}
	if n.Kind == JsonUnquoteOperation {
		// Keep explicit JSON null when decoding a declared ,string property.
		return "CASE WHEN " + args[0] + " = 'null'::jsonb THEN 'null'::jsonb ELSE CAST((" + args[0] + " #>> '{}') AS jsonb) END", nil
	}
	spec, _ := n.Kind.Spec()
	if spec.Infix {
		return "(" + args[0] + " " + spec.Name + " " + args[1] + ")", nil
	}
	return spec.Name + "(" + args[0] + ")", nil
}
