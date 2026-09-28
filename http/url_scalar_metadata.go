package http

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Normalize validates and owns a serialized scalar description. It promises
// only the declared URL syntax; CustomURLSyntax still needs its native codec
// or an explicitly supplied equivalent client codec.
func (info URLScalarInfo) Normalize() (URLScalarInfo, error) {
	shape, err := contract.DefineScalar[struct{}](info.Value).Description()
	if err != nil {
		return URLScalarInfo{}, err
	}
	valid := false
	switch info.Syntax {
	case TextURLSyntax:
		valid = shape.Kind == contract.StringKind
	case IntegerURLSyntax:
		valid = shape.Kind == contract.IntegerKind
	case FloatURLSyntax:
		valid = shape.Kind == contract.NumberKind
	case BooleanURLSyntax:
		valid = shape.Kind == contract.BooleanKind
	case ModelIDURLSyntax:
		valid = shape.Kind == contract.StringKind && shape.Format == contract.UUIDFormat
	case EnumURLSyntax:
		valid = len(shape.Cases) != 0
	case FormattedURLSyntax:
		valid = shape.Kind == contract.StringKind && shape.Format != ""
	case CustomURLSyntax:
		valid = true
	}
	if !valid {
		return URLScalarInfo{}, fault.New(fault.Invalid, "URL syntax disagrees with its scalar description")
	}
	info.Value = shape
	return info, nil
}
