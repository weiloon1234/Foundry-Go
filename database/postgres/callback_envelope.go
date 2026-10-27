package postgres

import (
	"database/sql/driver"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
	"reflect"
)

// A closure hides the value from pgx's argument formatting; the empty array keeps
// the original Go type in pgx's plan cache key without exposing any of its data.
// StructOf adds no methods, so this does not depend on promoted-method support.
type envelopeSource func() any

func sourceEnvelope(source any) any {
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "Source", Type: reflect.TypeFor[envelopeSource]()},
		{Name: "Shape", Type: reflect.ArrayOf(0, reflect.TypeOf(source))},
	})
	value := reflect.New(typ).Elem()
	value.Field(0).Set(reflect.ValueOf(envelopeSource(func() any { return source })))
	return value.Interface()
}
func envelopeValue(value any) (any, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil, false
	}
	typ := rv.Type()
	if typ.NumField() != 2 || typ.Field(0).Name != "Source" || typ.Field(0).Type != reflect.TypeFor[envelopeSource]() || typ.Field(1).Name != "Shape" || typ.Field(1).Type.Kind() != reflect.Array || typ.Field(1).Type.Len() != 0 {
		return nil, false
	}
	source := rv.Field(0).Interface().(envelopeSource)
	if source == nil {
		return nil, false
	}
	return source(), true
}
func inputValue(value any) (any, bool) {
	if input, ok := value.(protectedInput); ok {
		return input.originalValue(), true
	}
	return envelopeValue(value)
}
func outputValue(value any) (any, bool) {
	if output, ok := value.(protectedOutput); ok {
		return output.originalTarget(), true
	}
	return envelopeValue(value)
}
func wrapEnvelopeEncode(value any) (pgtype.WrappedEncodePlanNextSetter, any, bool) {
	source, ok := envelopeValue(value)
	if !ok {
		return nil, nil, false
	}
	return &envelopeEncodePlan{}, source, true
}

type envelopeEncodePlan struct{ next pgtype.EncodePlan }

func (p *envelopeEncodePlan) SetNext(next pgtype.EncodePlan) { p.next = next }
func (p *envelopeEncodePlan) Encode(value any, buf []byte) (result []byte, err error) {
	err = sqlcallback.Invoke("encode PostgreSQL custom argument", func() error {
		source, _ := envelopeValue(value)
		var e error
		result, e = p.next.Encode(source, buf)
		return e
	})
	return
}
func wrapEnvelopeScan(value any) (pgtype.WrappedScanPlanNextSetter, any, bool) {
	source, ok := envelopeValue(value)
	if !ok {
		return nil, nil, false
	}
	return &envelopeScanPlan{}, source, true
}

type envelopeScanPlan struct{ next pgtype.ScanPlan }

func (p *envelopeScanPlan) SetNext(next pgtype.ScanPlan) { p.next = next }
func (p *envelopeScanPlan) Scan(src []byte, value any) error {
	return sqlcallback.Invoke("scan PostgreSQL custom destination", func() error {
		target, _ := envelopeValue(value)
		return p.next.Scan(src, target)
	})
}

// Native codecs have already had the first opportunity to select their preferred
// interfaces when pgx reaches this fallback. Do not let its driver.Valuer plan
// format an invalid returned value while trying another native encoding.
func wrapCheckedValuer(value any) (pgtype.WrappedEncodePlanNextSetter, any, bool) {
	if _, checked := value.(checkedValuer); checked {
		return nil, nil, false
	}
	source, ok := value.(driver.Valuer)
	if !ok {
		return nil, nil, false
	}
	return &checkedValuerPlan{}, checkedValuer{source}, true
}

type checkedValuerPlan struct{ next pgtype.EncodePlan }

func (p *checkedValuerPlan) SetNext(next pgtype.EncodePlan) { p.next = next }
func (p *checkedValuerPlan) Encode(value any, buf []byte) ([]byte, error) {
	return p.next.Encode(checkedValuer{value.(driver.Valuer)}, buf)
}
