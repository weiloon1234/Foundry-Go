package postgres

import (
	"database/sql/driver"
	"encoding/json"
	"reflect"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/weiloon1234/Foundry-Go/internal/jsoncodec"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

type protectedInput interface{ originalValue() any }
type protectedOutput interface{ originalTarget() any }

// PostgreSQL reserves OIDs below FirstNormalObjectId for built-in objects. Each
// connection gets owned codec descriptors; the pgx shared defaults remain immutable.
const firstNormalObjectID uint32 = 16384
const protectedArgumentType = "foundry_protected_argument"

func protectCodecMap(m *pgtype.Map) {
	registry := codecRegistryFor(m)
	for oid := uint32(1); oid < firstNormalObjectID; oid++ {
		if typ, ok := m.TypeForOID(oid); ok {
			if _, protected := typ.Codec.(protectedCodec); !protected {
				registry.register(typ)
			}
		}
	}
	encode := []pgtype.TryWrapEncodePlanFunc{wrapEnvelopeEncode, wrapCheckedValuer}
	for _, fn := range m.TryWrapEncodePlanFuncs {
		pointer := reflect.ValueOf(fn).Pointer()
		if pointer != reflect.ValueOf(wrapEnvelopeEncode).Pointer() && pointer != reflect.ValueOf(wrapCheckedValuer).Pointer() {
			encode = append(encode, fn)
		}
	}
	m.TryWrapEncodePlanFuncs = encode
	scan := []pgtype.TryWrapScanPlanFunc{wrapEnvelopeScan}
	for _, fn := range m.TryWrapScanPlanFuncs {
		if reflect.ValueOf(fn).Pointer() != reflect.ValueOf(wrapEnvelopeScan).Pointer() {
			scan = append(scan, fn)
		}
	}
	m.TryWrapScanPlanFuncs = scan
}
func protectCodecType(m *pgtype.Map, typ *pgtype.Type) {
	if typ != nil {
		codecRegistryFor(m).register(typ)
	}
}
func protectValue(m *pgtype.Map, v any) any {
	if v == nil {
		return nil
	}
	if _, ok := inputValue(v); ok {
		return v
	}
	switch source := v.(type) {
	case pgtype.ArrayGetter:
		return protectedArray{arrayGetter: arrayGetter{source: source, m: m}, original: v}
	case pgtype.MultirangeGetter:
		return protectedMultirange{multirangeGetter: multirangeGetter{source: source, m: m}, original: v}
	case pgtype.RangeValuer:
		return protectedRange{rangeGetter: rangeGetter{source: source, m: m}, original: v}
	case pgtype.CompositeIndexGetter:
		return protectedComposite{compositeGetter: compositeGetter{source: source, m: m}, original: v}
	}
	wrapped := sourceEnvelope(v)
	if _, ok := m.TypeForValue(wrapped); !ok {
		m.RegisterDefaultPgType(wrapped, protectedArgumentType)
	}
	return wrapped
}
func protectTarget(v any) any {
	if v == nil {
		return nil
	}
	if _, ok := outputValue(v); ok {
		return v
	}
	switch source := v.(type) {
	case pgtype.ArraySetter:
		return protectedArrayTarget{arraySetter: arraySetter{source: source}, original: v}
	case pgtype.MultirangeSetter:
		return protectedMultirangeTarget{multirangeSetter: multirangeSetter{source: source}, original: v}
	case pgtype.RangeScanner:
		return protectedRangeTarget{rangeSetter: rangeSetter{source: source}, original: v}
	case pgtype.CompositeIndexScanner:
		return protectedCompositeTarget{compositeSetter: compositeSetter{source: source}, original: v}
	}
	return sourceEnvelope(v)
}

type protectedCodec struct {
	native     pgtype.Codec
	registry   *codecRegistry
	descriptor *pgtype.Type
}

func (c protectedCodec) FormatSupported(format int16) bool {
	if c.native == nil {
		return true
	}
	return c.native.FormatSupported(format)
}
func (c protectedCodec) PreferredFormat() int16 {
	if c.native == nil {
		return pgtype.TextFormatCode
	}
	return c.native.PreferredFormat()
}
func (c protectedCodec) PlanEncode(m *pgtype.Map, oid uint32, format int16, v any) pgtype.EncodePlan {
	if c.native == nil && c.registry != nil {
		if _, ok := v.(registrationProbe); ok {
			return &registrationPlan{marker: 1}
		}
	}
	if _, ok := inputValue(v); ok {
		return encodeValuePlan{m: m, oid: oid, format: format, descriptor: c.descriptor}
	}
	if c.native == nil {
		return nil
	}
	var plan pgtype.EncodePlan
	err := sqlcallback.Invoke("plan PostgreSQL encoding", func() error { plan = c.native.PlanEncode(m, oid, format, encodeContainer(m, c.native, v)); return nil })
	if err != nil {
		return failedEncodePlan{err}
	}
	if plan == nil {
		return nil
	}
	return encodeNativePlan{m: m, codec: c.native, native: plan}
}
func (c protectedCodec) PlanScan(m *pgtype.Map, oid uint32, format int16, v any) pgtype.ScanPlan {
	if _, ok := outputValue(v); ok {
		return scanValuePlan{m: m, oid: oid, format: format}
	}
	if c.native == nil {
		return nil
	}
	var plan pgtype.ScanPlan
	err := sqlcallback.Invoke("plan PostgreSQL scanning", func() error { plan = c.native.PlanScan(m, oid, format, scanContainer(c.native, v)); return nil })
	if err != nil {
		return failedScanPlan{err}
	}
	if plan == nil {
		return nil
	}
	return scanNativePlan{codec: c.native, native: plan}
}
func (c protectedCodec) DecodeDatabaseSQLValue(m *pgtype.Map, oid uint32, format int16, src []byte) (value driver.Value, err error) {
	err = sqlcallback.Invoke("decode PostgreSQL value", func() error {
		var e error
		value, e = c.decodingCodec(format).DecodeDatabaseSQLValue(m, oid, format, src)
		return e
	})
	return
}
func (c protectedCodec) DecodeValue(m *pgtype.Map, oid uint32, format int16, src []byte) (value any, err error) {
	err = sqlcallback.Invoke("decode PostgreSQL value", func() error {
		var e error
		value, e = c.decodingCodec(format).DecodeValue(m, oid, format, src)
		return e
	})
	return
}

type encodeValuePlan struct {
	m          *pgtype.Map
	oid        uint32
	format     int16
	descriptor *pgtype.Type
}

func (p encodeValuePlan) Encode(v any, buf []byte) (result []byte, err error) {
	err = sqlcallback.Invoke("encode PostgreSQL argument", func() error {
		source, _ := inputValue(v)
		rv := reflect.ValueOf(source)
		if !rv.IsValid() {
			return nil
		}
		oid := p.oid
		selected := p.descriptor
		if oid == 0 {
			selected, _ = p.m.TypeForValue(source)
			if selected != nil {
				oid = selected.OID
			}
		}
		switch rv.Kind() {
		case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.UnsafePointer, reflect.Interface, reflect.Slice:
			if rv.IsNil() {
				if _, ok := source.(driver.Valuer); !ok || rv.Kind() == reflect.Pointer && rv.Type().Elem().Implements(reflect.TypeFor[driver.Valuer]()) {
					return nil
				}
			}
		}

		// driver.Valuer is pgx's final conversion fallback; validate its result before
		// pgx can recursively format an invalid driver.Value.
		candidate := reflect.ValueOf(source)
		if _, textPreferred := source.(pgtype.TextValuer); textPreferred && p.format == pgtype.TextFormatCode {
			candidate = reflect.Value{}
		}
		for depth := 0; candidate.IsValid() && depth < 64; depth++ {
			if candidate.CanInterface() {
				if valuer, ok := candidate.Interface().(driver.Valuer); ok {
					var native pgtype.EncodePlan
					if selected != nil {
						if codec, ok := selected.Codec.(protectedCodec); ok && codec.native != nil {
							switch codec.native.(type) {
							case *pgtype.JSONCodec, *pgtype.JSONBCodec, *pgtype.XMLCodec:
								// These codecs prefer driver.Valuer themselves, before pgx's
								// wrapping hooks. Validate that result at the same boundary.
							default:
								native = codec.native.PlanEncode(p.m, oid, p.format, candidate.Interface())
							}
						}
					}
					if native == nil {
						source = checkedValuer{valuer}
					}
					break
				}
			}
			if candidate.Kind() != reflect.Pointer || candidate.IsNil() {
				break
			}
			candidate = candidate.Elem()
		}

		var plan pgtype.EncodePlan
		current, _ := p.m.TypeForOID(oid)
		if selected != nil && current != selected {
			plan = planSelectedType(p.m, selected, p.format, source)
		} else {
			plan = p.m.PlanEncode(oid, p.format, source)
		}
		if plan == nil {
			return safeerror.New("unsupported PostgreSQL argument")
		}
		var e error
		result, e = plan.Encode(source, buf)
		return e
	})
	return
}

// Native inference selects a name-mapped descriptor before resolving wrapper
// values by OID. Preserve that order when an alias shares another type's OID.
func planSelectedType(m *pgtype.Map, typ *pgtype.Type, format int16, source any) pgtype.EncodePlan {
	if format == pgtype.TextFormatCode {
		switch source.(type) {
		case string, pgtype.TextValuer:
			return pgtype.TextCodec{}.PlanEncode(m, typ.OID, format, source)
		}
	}
	if plan := typ.Codec.PlanEncode(m, typ.OID, format, source); plan != nil {
		return plan
	}
	for _, wrap := range m.TryWrapEncodePlanFuncs {
		if plan, next, ok := wrap(source); ok {
			if nextPlan := m.PlanEncode(typ.OID, format, next); nextPlan != nil {
				plan.SetNext(nextPlan)
				return plan
			}
		}
	}
	// checkedValuer is already opaque and validated; pgx owns its final fallback.
	if _, ok := source.(checkedValuer); ok {
		return m.PlanEncode(typ.OID, format, source)
	}
	return nil
}

type checkedValuer struct{ source driver.Valuer }

func (v checkedValuer) Value() (result driver.Value, err error) {
	rv := reflect.ValueOf(v.source)
	if rv.Kind() == reflect.Pointer && rv.IsNil() && rv.Type().Elem().Implements(reflect.TypeFor[driver.Valuer]()) {
		return nil, nil
	}

	err = sqlcallback.Invoke("convert PostgreSQL argument", func() error {
		var e error
		result, e = v.source.Value()
		if e != nil {
			return e
		}
		if !driver.IsValue(result) {
			return safeerror.New("invalid SQL driver value")
		}
		return nil
	})
	return
}

type encodeNativePlan struct {
	m      *pgtype.Map
	codec  pgtype.Codec
	native pgtype.EncodePlan
}

func (p encodeNativePlan) Encode(v any, buf []byte) (result []byte, err error) {
	err = sqlcallback.Invoke("encode PostgreSQL value", func() error {
		var e error
		result, e = p.native.Encode(encodeContainer(p.m, p.codec, v), buf)
		return e
	})
	return
}

type scanValuePlan struct {
	m      *pgtype.Map
	oid    uint32
	format int16
}

func (p scanValuePlan) Scan(src []byte, v any) error {
	return sqlcallback.Invoke("scan PostgreSQL destination", func() error { target, _ := outputValue(v); return p.m.Scan(p.oid, p.format, src, target) })
}

type scanNativePlan struct {
	codec  pgtype.Codec
	native pgtype.ScanPlan
}

func (p scanNativePlan) Scan(src []byte, v any) error {
	return sqlcallback.Invoke("scan PostgreSQL value", func() error { return p.native.Scan(src, scanContainer(p.codec, v)) })
}

type failedEncodePlan struct{ err error }

func (p failedEncodePlan) Encode(any, []byte) ([]byte, error) { return nil, p.err }

type failedScanPlan struct{ err error }

func (p failedScanPlan) Scan([]byte, any) error { return p.err }

func (c protectedCodec) decodingCodec(format int16) pgtype.Codec {
	if c.native != nil {
		return c.native
	}
	if format == pgtype.BinaryFormatCode {
		return pgtype.ByteaCodec{}
	}
	return pgtype.TextCodec{}
}

// Preserve explicitly registered callback behavior. Only replace pgx's known
// standard JSON callbacks, whose internal error formatting needs interception.
func protectedJSONMarshal(fn func(any) ([]byte, error)) func(any) ([]byte, error) {
	if reflect.ValueOf(fn).Pointer() == reflect.ValueOf(json.Marshal).Pointer() {
		return func(value any) ([]byte, error) { return jsoncodec.Marshal(value) }
	}
	return fn
}
func protectedJSONUnmarshal(fn func([]byte, any) error) func([]byte, any) error {
	if reflect.ValueOf(fn).Pointer() == reflect.ValueOf(json.Unmarshal).Pointer() {
		return func(data []byte, value any) error { return jsoncodec.Unmarshal(data, value) }
	}
	return fn
}
