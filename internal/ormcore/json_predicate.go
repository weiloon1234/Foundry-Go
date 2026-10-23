package ormcore

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// JSONContainment uses the existing JSON operation compiler for a bound document.
// Example: JSONContainment(field, querycore.JsonContainsOperation, document, nil).
func JSONContainment(field querycore.FieldRef, op querycore.ScalarOperation, document driver.Value, err error) querycore.Expression {
	return querycore.BinaryComparison{
		Left: querycore.OperationNode{Kind: op, Result: codec.TypeBoolean, Arguments: []querycore.OperationArgument{
			{Value: field, Kind: codec.TypeJSON},
			{Value: querycore.ParameterNode{Kind: codec.TypeJSON, Value: document, Err: err}, Kind: codec.TypeJSON},
		}},
		Right: querycore.ParameterNode{Kind: codec.TypeBoolean, Value: true}, Operator: querycore.Equal,
	}
}

// JSONIsNull tests a document's JSON kind; SQL NULL remains unknown and does not match.
// Example: JSONIsNull(field).
func JSONIsNull(field querycore.FieldRef) querycore.Expression {
	return querycore.BinaryComparison{
		Left:  querycore.OperationNode{Kind: querycore.JsonKindOperation, Result: codec.TypeText, Arguments: []querycore.OperationArgument{{Value: field, Kind: codec.TypeJSON}}},
		Right: querycore.ParameterNode{Kind: codec.TypeText, Value: "null"}, Operator: querycore.Equal,
	}
}
