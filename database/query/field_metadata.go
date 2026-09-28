package query

import (
	"database/sql/driver"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// RecordField supplies typed field extraction and codec validation at the
// generated model/projection boundary, shared by cursors, chunks and relation keys.
type RecordField[M any] struct {
	column          string
	typ             reflect.Type
	kind            codec.ParameterType
	sensitive       bool
	get             func(M) (driver.Value, error)
	decode          func(any) (driver.Value, error)
	assignment      func(any) (assignmentValue, error)
	mutator         fieldMutator
	conflictMutator fieldMutator
}

// ModelField preserves the model-facing name of shared record field metadata.
type ModelField[M any] = RecordField[M]

func (d Definition[M]) modelField(column string) (ModelField[M], bool) {
	for _, f := range d.modelFields {
		if f.column == column {
			return f, true
		}
	}
	return ModelField[M]{}, false
}

// encodeModelKey preserves canonical driver representations while agreeing
// with Go/SQL equality for floating-point signed zero. Cursor tokens retain
// their separate transport representation and are not used as public keys.
func encodeModelKey(raw driver.Value) (cursorValue, error) {
	if f, ok := raw.(float64); ok && f == 0 {
		raw = float64(0)
	}
	key, err := encodeCursorValue(raw)
	if err != nil {
		return cursorValue{}, fault.New(fault.Invalid, "model key has no canonical database representation")
	}
	return key, nil
}

func (f RecordField[M]) equalityKey(raw driver.Value) (cursorValue, error) {
	if f.kind == codec.TypeInterval && raw != nil {
		interval, err := codec.Interval().Decode(raw)
		if err != nil {
			return cursorValue{}, err
		}
		return cursorValue{Kind: "interval", Text: intervalComparisonKey(interval)}, nil
	}
	return encodeModelKey(raw)
}

// NewModelField retains the concrete field type through extraction and codec
// validation. Applications use generated fields rather than these declarations.
func NewModelField[M, V any](column string, c codec.Codec[V], get func(M) V) ModelField[M] {
	return NewRecordField(column, c, get)
}

// NewRecordField retains a result field's exact type, getter and codec. Generated
// declarations supply these together with the complete result decoder.
func NewRecordField[M, V any](column string, c codec.Codec[V], get func(M) V) RecordField[M] {
	if get == nil || c.Validate() != nil {
		return ModelField[M]{}
	}
	return RecordField[M]{column: column, typ: reflect.TypeFor[V](), kind: c.ParameterType(), sensitive: c.SensitiveValues(), get: func(m M) (driver.Value, error) {
		return c.Bind(get(m))
	}, decode: func(raw any) (driver.Value, error) {
		v, err := c.Decode(raw)
		if err != nil {
			return nil, err
		}
		return c.Bind(v)
	}, assignment: func(raw any) (assignmentValue, error) {
		v, err := c.Decode(raw)
		if err != nil {
			return assignmentValue{}, err
		}
		return captureAssignment(c, v), nil
	}}
}
