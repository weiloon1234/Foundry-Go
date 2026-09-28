package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type parameterNode struct {
	kind  codec.ParameterType
	value driver.Value
	err   error
}

func (parameterNode) valueNode() {}

func parameterSQLType(kind codec.ParameterType) (string, error) {
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
func (p parameterNode) validate() error {
	if p.err != nil {
		return p.err
	}
	if !driver.IsValue(p.value) {
		return fault.New(fault.Invalid, "parameter expression has an invalid driver value")
	}
	_, err := parameterSQLType(p.kind)
	return err
}
func (c *compiler) parameterSQL(p parameterNode) (string, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	typeName, _ := parameterSQLType(p.kind)
	placeholder, err := c.parameter(p.value)
	return "CAST(" + placeholder + " AS " + typeName + ")", err
}
