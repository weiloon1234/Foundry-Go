package ormcore

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

type namedInt int
type namedInt8 int8
type namedInt16 int16
type namedInt32 int32
type namedInt64 int64
type namedUint uint
type namedUint8 uint8
type namedUint16 uint16
type namedUint32 uint32
type namedUint64 uint64
type namedFloat32 float32
type namedFloat64 float64
type namedString string
type namedBool bool

func equivalent[V any](t *testing.T, got, want codec.Codec[V], v V, raw ...any) {
	t.Helper()
	if got.Validate() != nil || got.ParameterType() != want.ParameterType() || got.SensitiveValues() != want.SensitiveValues() || got.ComparesValues() != want.ComparesValues() {
		t.Fatal("codec metadata")
	}
	a, e := got.Bind(v)
	b, f := want.Bind(v)
	if !reflect.DeepEqual(a, b) || (e == nil) != (f == nil) {
		t.Fatalf("bind %v %v / %v %v", a, e, b, f)
	}
	if !reflect.DeepEqual(got.Clone(v), want.Clone(v)) {
		t.Fatal("clone")
	}
	for _, r := range raw {
		a, e := got.Decode(r)
		b, f := want.Decode(r)
		if !reflect.DeepEqual(a, b) || (e == nil) != (f == nil) {
			t.Fatalf("decode %v: %v %v / %v %v", r, a, e, b, f)
		}
		if e != nil && !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
		dst := v
		err := got.Scan(&dst).Scan(r)
		if err != nil && !reflect.DeepEqual(dst, v) {
			t.Fatal("failed scan mutated destination")
		}
	}
}

func TestPrimitiveCodecParity(t *testing.T) {
	signed := []any{int64(-12), "127", "128", "32768", "2147483648", nil, float64(1), "bad"}
	equivalent(t, NumberCodec[namedInt](), codec.Signed[namedInt](), namedInt(4096), signed...)
	equivalent(t, NumberCodec[namedInt8](), codec.Signed[namedInt8](), namedInt8(-4), signed...)
	equivalent(t, NumberCodec[namedInt16](), codec.Signed[namedInt16](), namedInt16(4096), signed...)
	equivalent(t, NumberCodec[namedInt32](), codec.Signed[namedInt32](), namedInt32(4096), signed...)
	equivalent(t, NumberCodec[namedInt64](), codec.Signed[namedInt64](), namedInt64(4096), signed...)
	unsigned := []any{int64(-1), int64(4096), "255", "256", "65536", "4294967296", nil, float64(1)}
	equivalent(t, NumberCodec[namedUint](), codec.Unsigned[namedUint](), namedUint(4096), unsigned...)
	equivalent(t, NumberCodec[namedUint8](), codec.Unsigned[namedUint8](), namedUint8(4), unsigned...)
	equivalent(t, NumberCodec[namedUint16](), codec.Unsigned[namedUint16](), namedUint16(4096), unsigned...)
	equivalent(t, NumberCodec[namedUint32](), codec.Unsigned[namedUint32](), namedUint32(4096), unsigned...)
	equivalent(t, NumberCodec[namedUint64](), codec.Unsigned[namedUint64](), namedUint64(4096), unsigned...)
	floats := []any{1.25, math.MaxFloat64, math.SmallestNonzeroFloat64, math.NaN(), math.Inf(1), int64(1), nil}
	equivalent(t, NumberCodec[namedFloat32](), codec.Float[namedFloat32](), namedFloat32(1.25), floats...)
	equivalent(t, NumberCodec[namedFloat64](), codec.Float[namedFloat64](), namedFloat64(1.25), floats...)
	equivalent(t, KeyScalarCodec[namedString](), codec.String[namedString](), namedString("text"), "text", []byte("ok"), "\x00", "\xff", nil)
	equivalent(t, ScalarCodec[namedBool](), codec.Bool[namedBool](), namedBool(true), true, false, 1, nil)
}
func TestFrameworkCodecsAndInvalidFamilies(t *testing.T) {
	equivalent(t, NumberCodec[decimal.Decimal](), codec.Decimal(), decimal.FromInt64(123), "1.25", nil, "bad")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	equivalent(t, TimeCodec[time.Time](), codec.Time(), now, now, nil, "bad")
	if TimeCodec[temporal.Date]().ParameterType() != codec.Date().ParameterType() {
		t.Fatal("date metadata")
	}
	if TimeCodec[temporal.DateTime]().ParameterType() != codec.DateTime().ParameterType() {
		t.Fatal("instant metadata")
	}
	if TimeCodec[temporal.Time]().ParameterType() != codec.WallTime().ParameterType() {
		t.Fatal("time metadata")
	}
	if TimeCodec[temporal.LocalDateTime]().ParameterType() != codec.LocalDateTime().ParameterType() {
		t.Fatal("local datetime metadata")
	}
	if NumberCodec[string]().Validate() == nil || TimeCodec[int64]().Validate() == nil || ScalarCodec[struct{ X int }]().Validate() == nil {
		t.Fatal("unsupported family accepted")
	}
	if _, err := NumberCodec[uint64]().Bind(math.MaxUint64); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsigned overflow", err)
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NumberCodec[float64]().Bind(v); !errors.Is(err, fault.Invalid) {
			t.Fatal("non-finite", err)
		}
	}
}

type badEnum string

func (badEnum) EnumDescriptor() enum.Descriptor[badEnum] { return enum.Descriptor[badEnum]{} }
func TestInvalidEnumDescriptorIsTyped(t *testing.T) {
	c, check := EnumCodec[badEnum]()
	if !errors.Is(check(), fault.Invalid) {
		t.Fatal(check())
	}
	if _, err := c.Bind("x"); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
