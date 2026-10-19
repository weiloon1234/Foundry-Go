package querycore

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type ParameterNode struct {
	Kind  codec.ParameterType
	Value driver.Value
	Err   error
	// literal renders a generated declaration constant, such as a JSON
	// property name, inline as an escaped schema literal instead of a bind
	// parameter, so expression indexes on the same path can match. Values
	// supplied at run time always remain bind parameters.
	Literal bool
}

func (ParameterNode) ValueNode() {}

func ParameterSQLType(kind codec.ParameterType) (string, error) {
	switch kind {
	case codec.TypeBoolean:
		return "boolean", nil
	case codec.TypeInteger:
		return "bigint", nil
	case codec.TypeFloat:
		return "double precision", nil
	case codec.TypeText:
		return "text", nil
	case codec.TypeUUID:
		return "uuid", nil
	case codec.TypeDecimal:
		return "numeric", nil
	case codec.TypeDate:
		return "date", nil
	case codec.TypeTime:
		return "time without time zone", nil
	case codec.TypeLocalDateTime:
		return "timestamp without time zone", nil
	case codec.TypeDateTime:
		return "timestamp with time zone", nil
	case codec.TypeBytes:
		return "bytea", nil
	case codec.TypeInterval:
		return "interval", nil
	case codec.TypeJSON:
		return "jsonb", nil
	default:
		return "", fault.New(fault.Invalid, "parameter expression requires a supported codec parameter type")
	}
}

func (p ParameterNode) Validate() error {
	if p.Err != nil {
		return p.Err
	}
	if !driver.IsValue(p.Value) {
		return fault.New(fault.Invalid, "parameter expression has an invalid driver value")
	}
	_, err := ParameterSQLType(p.Kind)
	return err
}

func (c *Compiler) ParameterSQL(p ParameterNode) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	typeName, _ := ParameterSQLType(p.Kind)
	if p.Literal {
		text, err := c.SchemaLiteral(p.Value)
		return "CAST(" + text + " AS " + typeName + ")", err
	}
	placeholder, err := c.Parameter(p.Value)
	return "CAST(" + placeholder + " AS " + typeName + ")", err
}
