package postgres

import (
	"database/sql/driver"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

type formattedArgument struct {
	Secret string
	calls  *atomic.Int32
}

func (v formattedArgument) Format(fmt.State, rune) {
	if v.calls != nil {
		v.calls.Add(1)
	}
	panic("argument formatter invoked")
}
func (v formattedArgument) GoString() string {
	if v.calls != nil {
		v.calls.Add(1)
	}
	panic("argument GoString invoked")
}
func (v formattedArgument) Error() string {
	if v.calls != nil {
		v.calls.Add(1)
	}
	panic("argument Error invoked")
}
func TestPostgresEnvelopeHidesApplicationValues(t *testing.T) {
	for _, source := range []any{formattedArgument{Secret: "private-argument-contents"}, struct{ Secret string }{"private-argument-contents"}} {
		wrapped := sourceEnvelope(source)
		for _, format := range []string{"%v", "%+v", "%#v"} {
			rendered := fmt.Sprintf(format, wrapped)
			if strings.Contains(rendered, "private-argument-contents") || strings.Contains(rendered, "PANIC") {
				t.Fatal("argument data or formatting escaped envelope")
			}
		}
	}
}
func TestPostgresUninferredNativeScalarPlansRetainSourceTypes(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	plain := pgtype.NewMap()
	typ := &pgtype.Type{Name: "custom_integer", OID: 80003, Codec: pgtype.Int8Codec{}}
	protectCodecType(m, typ)
	plain.RegisterType(typ)
	for repeat := 0; repeat < 2; repeat++ {
		for _, source := range []any{int64(11), int32(12), pgtype.Int8{Int64: 13, Valid: true}, int64(14)} {
			for _, format := range []int16{pgtype.TextFormatCode, pgtype.BinaryFormatCode} {
				want, werr := plain.Encode(typ.OID, format, source, nil)
				got, gerr := m.Encode(typ.OID, format, protectValue(m, source), nil)
				if werr != nil || gerr != nil || string(want) != string(got) {
					t.Fatal("custom scalar encoding or cache source type changed")
				}
			}
		}
	}
	cause := &nativePrivateError{}
	_, err := m.Encode(typ.OID, pgtype.BinaryFormatCode, protectValue(m, failingInteger{cause}), nil)
	assertNativeCause(t, err, cause)
	var a int64
	var b int32
	for _, target := range []any{&a, &b, &a} {
		if err := m.Scan(typ.OID, pgtype.TextFormatCode, []byte("42"), protectTarget(target)); err != nil {
			t.Fatal("custom scalar scan target type changed")
		}
	}
	if a != 42 || b != 42 {
		t.Fatal("custom scalar scan value changed")
	}
	assertNativeCause(t, m.Scan(typ.OID, pgtype.TextFormatCode, []byte("42"), protectTarget(&failingNativeScanner{cause})), cause)
}

type customJSONValue struct{ value int }

func TestPostgresCustomJSONCallbacksRetained(t *testing.T) {
	for _, jsonb := range []bool{false, true} {
		m := pgtype.NewMap()
		protectCodecMap(m)
		marshalCalls, unmarshalCalls := 0, 0
		cause := &nativePrivateError{}
		failing := false
		marshal := func(value any) ([]byte, error) {
			marshalCalls++
			if _, ok := value.(customJSONValue); !ok {
				t.Fatal("custom JSON marshal source type changed")
			}
			if failing {
				return nil, cause
			}
			return []byte("{ \"custom\": true }"), nil
		}
		unmarshal := func(data []byte, target any) error {
			unmarshalCalls++
			if failing {
				return cause
			}
			destination, ok := target.(*customJSONValue)
			if !ok {
				t.Fatal("custom JSON unmarshal target changed")
			}
			destination.value = 42
			return nil
		}
		var codec pgtype.Codec = &pgtype.JSONCodec{Marshal: marshal, Unmarshal: unmarshal}
		if jsonb {
			codec = &pgtype.JSONBCodec{Marshal: marshal, Unmarshal: unmarshal}
		}
		typ := &pgtype.Type{Name: "custom_json", OID: 80004, Codec: codec}
		protectCodecType(m, typ)
		m.RegisterDefaultPgType(customJSONValue{}, typ.Name)
		encoded, err := m.Encode(typ.OID, pgtype.TextFormatCode, protectValue(m, customJSONValue{}), nil)
		if err != nil || string(encoded) != "{ \"custom\": true }" || marshalCalls != 1 {
			t.Fatal("custom JSON encoding changed")
		}
		var destination customJSONValue
		if err := m.Scan(typ.OID, pgtype.TextFormatCode, encoded, protectTarget(&destination)); err != nil || destination.value != 42 || unmarshalCalls != 1 {
			t.Fatal("custom JSON decoding changed")
		}
		failing = true
		_, err = m.Encode(typ.OID, pgtype.TextFormatCode, protectValue(m, customJSONValue{}), nil)
		assertNativeCause(t, err, cause)
		assertNativeCause(t, m.Scan(typ.OID, pgtype.TextFormatCode, encoded, protectTarget(&destination)), cause)
	}
}

type hostileXML struct{ cause error }

func (v hostileXML) MarshalXML(*xml.Encoder, xml.StartElement) error    { return v.cause }
func (v *hostileXML) UnmarshalXML(*xml.Decoder, xml.StartElement) error { return v.cause }
func TestPostgresXMLCallbackErrorsStayOpaque(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	for _, mode := range []string{"private", "panic", "goexit", "is_panic", "is_exit", "as_panic", "as_exit", "cycle"} {
		cause := &nativePrivateError{mode: mode}
		// pgx resets XML targets before unmarshalling. Restore the fixture's
		// external failure source after that reset, then exercise encoding/xml.
		typ := &pgtype.Type{Name: "xml", OID: pgtype.XMLOID, Codec: &pgtype.XMLCodec{
			Marshal: xml.Marshal,
			Unmarshal: func(data []byte, target any) error {
				target.(*hostileXML).cause = cause
				return xml.Unmarshal(data, target)
			},
		}}
		protectCodecType(m, typ)
		_, err := m.Encode(pgtype.XMLOID, pgtype.TextFormatCode, protectValue(m, hostileXML{cause}), nil)
		assertNativeCause(t, err, cause)
		assertNativeCause(t, m.Scan(pgtype.XMLOID, pgtype.TextFormatCode, []byte("<value/>"), protectTarget(&hostileXML{cause})), cause)
	}
}

func TestPostgresRegisteredJSONCodecUnwrapsBeforeBinding(t *testing.T) {
	for _, jsonb := range []bool{false, true} {
		m := pgtype.NewMap()
		protectCodecMap(m)
		var codec pgtype.Codec = &pgtype.JSONCodec{Marshal: json.Marshal, Unmarshal: json.Unmarshal}
		if jsonb {
			codec = &pgtype.JSONBCodec{Marshal: json.Marshal, Unmarshal: json.Unmarshal}
		}
		const oid uint32 = 80005
		protectCodecType(m, &pgtype.Type{Name: "uninferred_json", OID: oid, Codec: codec})
		data, err := m.Encode(oid, pgtype.TextFormatCode, protectValue(m, customJSONValue{}), nil)
		if err != nil || string(data) != "{}" {
			t.Fatal("registered catch-all codec did not receive original value")
		}
		var destination customJSONValue
		if err := m.Scan(oid, pgtype.TextFormatCode, []byte("{}"), protectTarget(&destination)); err != nil {
			t.Fatal("registered catch-all codec did not receive original target")
		}
	}
}

type invalidNativeValuer struct{ calls *atomic.Int32 }

func (v invalidNativeValuer) Value() (driver.Value, error) {
	return formattedArgument{Secret: "private-argument-contents", calls: v.calls}, nil
}

type preferredNativeInteger struct{}

func (preferredNativeInteger) Int64Value() (pgtype.Int8, error) {
	return pgtype.Int8{Int64: 42, Valid: true}, nil
}
func (preferredNativeInteger) Value() (driver.Value, error) {
	panic("driver.Valuer displaced native interface")
}
func TestPostgresCustomValuerFallbackPreservesNativePrecedence(t *testing.T) {
	m := pgtype.NewMap()
	protectCodecMap(m)
	const oid uint32 = 80006
	protectCodecType(m, &pgtype.Type{Name: "unmapped_integer", OID: oid, Codec: pgtype.Int8Codec{}})
	for _, format := range []int16{pgtype.TextFormatCode, pgtype.BinaryFormatCode} {
		data, err := m.Encode(oid, format, protectValue(m, preferredNativeInteger{}), nil)
		if err != nil {
			t.Fatal("native integer interface lost precedence")
		}
		var value int64
		if err := m.Scan(oid, format, data, protectTarget(&value)); err != nil || value != 42 {
			t.Fatal("native integer value changed")
		}
	}
	var calls atomic.Int32
	source := invalidNativeValuer{calls: &calls}
	pointer := &source
	for _, input := range []any{source, pointer, &pointer} {
		for _, oid := range []uint32{oid, pgtype.Int8OID, pgtype.JSONOID, pgtype.JSONBOID, pgtype.XMLOID} {
			data, err := m.Encode(oid, pgtype.TextFormatCode, protectValue(m, input), nil)
			if err == nil || data != nil {
				t.Fatal("invalid driver.Value accepted")
			}
			if calls.Load() != 0 || strings.Contains(err.Error(), "private-argument-contents") || strings.Contains(err.Error(), "PANIC") {
				t.Fatal("invalid driver.Value was formatted")
			}
		}
	}
}
