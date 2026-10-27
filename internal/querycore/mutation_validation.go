package querycore

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func (c *Compiler) AssignmentParameter(column string, bind func() (driver.Value, error)) (string, error) {
	bound, err := BindAssignment(c.Columns[column], bind)
	if err != nil {
		return "", err
	}
	return c.Parameter(bound)
}

func BindAssignment(column Column, bind func() (driver.Value, error)) (driver.Value, error) {
	bound, err := bind()
	if err != nil {
		return nil, err
	}
	if bound == nil && !column.Nullable {
		return nil, safeerror.NewFault(fault.Invalid, "NULL assignment requires a nullable model field")
	}
	return bound, nil
}
