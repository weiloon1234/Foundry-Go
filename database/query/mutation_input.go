package query

import (
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// AssignInput is a generated declaration boundary for a value that must pass
// through its field mutator before SQL binding. I need not have a database
// codec; the stored result's codec owns final validation and binding. Ordinary
// application code uses its generated draft's typed setter.
func AssignInput[M, I any](table, column string, input I) Assignment[M] {
	return Assignment[M]{field: fieldRef{table, column}, assignmentValue: captureInput(input)}
}

func captureInput[I any](input I) assignmentValue {
	return assignmentValue{value: input, bind: func() (driver.Value, error) {
		return nil, fault.New(fault.Invalid, "mutation input must be transformed before SQL binding")
	}}
}

// AssignClonedInput is the generated mutable-input boundary. The codec supplies
// snapshot ownership only: binding, decoding, validation and the field mutator
// do not run until their ordinary lifecycle phase.
func AssignClonedInput[M, I any](table, column string, c codec.Codec[I], input I) Assignment[M] {
	return Assignment[M]{field: fieldRef{table, column}, assignmentValue: captureClonedInput(c, input)}
}

func captureClonedInput[I any](c codec.Codec[I], input I) assignmentValue {
	input = c.Clone(input)
	a := captureInput(input)
	a.copyValue = func() any { return c.Clone(input) }
	return a
}

// MutationInputField supplies a generated conflict setter whose input differs
// from its stored query value. The generated field keeps its stored comparison,
// ordering and Incoming capabilities while delegating Set to this descriptor.
type MutationInputField[M, I any] struct {
	_          [0]*M
	_          [0]*I
	ref        fieldRef
	cloneCodec *codec.Codec[I]
}

// NewMutationInputField declares a model-owned input boundary. Generated code
// supplies the table/column from the existing model metadata; no input codec or
// additional application configuration is required.
func NewMutationInputField[M, I any](table, column string) MutationInputField[M, I] {
	return MutationInputField[M, I]{ref: fieldRef{table, column}}
}

// NewClonedMutationInputField declares the same input capability with codec
// ownership for mutable inputs. It does not enable binding the input to SQL.
func NewClonedMutationInputField[M, I any](table, column string, c codec.Codec[I]) MutationInputField[M, I] {
	return MutationInputField[M, I]{ref: fieldRef{table, column}, cloneCodec: &c}
}

// Set captures fresh input for a conflict literal. The normal mutation pipeline
// transforms it once before SQL; constructing this value invokes no user code.
func (f MutationInputField[M, I]) Set(input I) ConflictUpdate[M] {
	if f.cloneCodec != nil {
		return literalConflict[M](f.ref, captureClonedInput(*f.cloneCodec, input))
	}
	return literalConflict[M](f.ref, captureInput(input))
}
