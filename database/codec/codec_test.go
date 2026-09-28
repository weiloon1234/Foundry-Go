package codec_test

import (
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type code string
type small int8
type owner struct{}

func TestScanPublishesOnlySuccessfulTypedValues(t *testing.T) {
	c := codec.String[code]().Validated(func(v code) error {
		if v != "active" && v != "disabled" {
			return fault.New(fault.Invalid, "unknown state")
		}
		return nil
	})
	current := code("active")
	for _, input := range []any{nil, "unknown", 42, true, []byte{0xff}} {
		if err := c.Scan(&current).Scan(input); err == nil || current != "active" {
			t.Fatal("failed decode changed destination")
		}
	}
	if err := c.Scan(&current).Scan([]byte("disabled")); err != nil || current != "disabled" {
		t.Fatal("valid byte value not decoded")
	}
	if _, err := c.Bind("unknown"); !errors.Is(err, fault.Invalid) {
		t.Fatal("write skipped validator")
	}
	if err := c.Scan(nil).Scan("active"); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil scan destination accepted")
	}
	nullable := codec.Nullable(c)
	for _, input := range []value.Nullable[code]{value.Null[code](), value.Of(code("active"))} {
		encoded, err := nullable.Bind(input)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := nullable.Decode(encoded)
		if err != nil || decoded != input {
			t.Fatal("nullable state lost")
		}
	}
	var absent codec.Codec[string]
	if _, err := codec.Nullable(absent).Bind(value.Null[string]()); err == nil {
		t.Fatal("NULL hid an undeclared codec")
	}
}

func TestScalarRangesAndRepresentationBoundaries(t *testing.T) {
	for _, input := range []any{int64(-129), int64(128), float64(1), "1.5", "9223372036854775808", nil} {
		if _, err := codec.Signed[small]().Decode(input); err == nil {
			t.Fatal("signed width or representation boundary skipped")
		}
	}
	if v, err := codec.Signed[small]().Decode([]byte("-128")); err != nil || v != -128 {
		t.Fatal("signed lower bound rejected")
	}
	for _, input := range []any{int64(-1), int64(256), "18446744073709551615"} {
		if _, err := codec.Unsigned[uint8]().Decode(input); err == nil {
			t.Fatal("unsigned range boundary skipped")
		}
	}
	if _, err := codec.Unsigned[uint64]().Bind(math.MaxUint64); err == nil {
		t.Fatal("unsigned value exceeds SQL signed range")
	}
	if v, err := codec.Unsigned[uint64]().Bind(math.MaxInt64); err != nil || v != int64(math.MaxInt64) {
		t.Fatal("valid upper bound rejected")
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.MaxFloat64, math.SmallestNonzeroFloat64} {
		if _, err := codec.Float[float32]().Decode(v); err == nil {
			t.Fatal("float bound skipped")
		}
	}
	if _, err := codec.Float[float64]().Decode(int64(9007199254740993)); err == nil {
		t.Fatal("integer silently rounded through float")
	}
	if _, err := codec.Bool[bool]().Decode("true"); err == nil {
		t.Fatal("boolean used implicit text coercion")
	}
	for _, v := range []string{"contains\x00nul", "\xff"} {
		if _, err := codec.String[string]().Bind(v); err == nil {
			t.Fatal("invalid PostgreSQL text accepted")
		}
	}
}

func TestIDsDecimalsAndBufferOwnership(t *testing.T) {
	id, err := model.NewID[owner]()
	if err != nil {
		t.Fatal(err)
	}
	bound, err := codec.ID[owner]().Bind(id)
	if err != nil {
		t.Fatal(err)
	}
	if restored, err := codec.ID[owner]().Decode(bound); err != nil || restored != id {
		t.Fatal("model-owned UUID changed")
	}
	if _, err := codec.Decimal().Decode(float64(1.25)); err == nil {
		t.Fatal("float accepted as exact decimal")
	}
	data := []byte("9007199254740993.123456789")
	parsed, err := codec.Decimal().Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = '1'
	if parsed.String() != "9007199254740993.123456789" {
		t.Fatal("decimal retained driver buffer")
	}
	if decoded, err := codec.Decimal().Decode(int64(42)); err != nil || decoded != decimal.FromInt64(42) {
		t.Fatal("integer decimal conversion lost exactness")
	}
	custom := codec.New(func(v []byte) (driver.Value, error) { return v, nil }, func(v any) ([]byte, error) { return nil, nil })
	buffer := []byte("before")
	result, err := custom.Bind(buffer)
	if err != nil {
		t.Fatal(err)
	}
	buffer[0] = 'a'
	if string(result.([]byte)) != "before" {
		t.Fatal("bound bytes retained mutable source")
	}
	for _, input := range [][]byte{nil, {}} {
		bound, err := custom.Bind(input)
		if err != nil {
			t.Fatal(err)
		}
		if input == nil {
			if bound != nil {
				t.Fatal("nil bytes did not bind as SQL NULL")
			}
			continue
		}
		if output, ok := bound.([]byte); !ok || output == nil || len(output) != 0 {
			t.Fatal("empty bytes did not remain a non-null empty buffer")
		}
	}
}

func TestTemporalMeaningPrecisionAndFailedScans(t *testing.T) {
	zone := time.FixedZone("explicit", 8*3600)
	instant := time.Date(2026, 9, 11, 12, 34, 56, 123456000, zone)
	bound, err := codec.Time().Bind(instant)
	if err != nil || !bound.(time.Time).Equal(instant) || bound.(time.Time).Location() != time.UTC {
		t.Fatal("instant was not normalized to UTC")
	}
	wall, err := codec.LocalDateTime().Decode(instant)
	if err != nil || wall.String() != "2026-09-11T12:34:56.123456" {
		t.Fatal("local wall time was converted to another zone")
	}
	if _, err := codec.Time().Bind(instant.Add(time.Nanosecond)); err == nil {
		t.Fatal("sub-microsecond instant silently truncated")
	}
	nanosecond, err := temporal.NewDateTime(instant.Add(time.Nanosecond))
	if err != nil || !nanosecond.UTC().Equal(instant.Add(time.Nanosecond)) {
		t.Fatal("temporal instant lost nanosecond precision", err)
	}
	if _, err := codec.DateTime().Bind(nanosecond); err == nil {
		t.Fatal("sub-microsecond DateTime silently truncated")
	}
	if _, err := codec.Date().Decode(instant); err == nil {
		t.Fatal("time silently discarded from calendar date")
	}
	if _, err := codec.WallTime().Decode("24:00:00"); err == nil {
		t.Fatal("unsupported PostgreSQL wall time accepted")
	}
	var current temporal.DateTime
	if err := codec.DateTime().Scan(&current).Scan("infinity"); err == nil || !current.IsZero() {
		t.Fatal("infinite instant accepted or changed receiver")
	}
	if _, err := codec.Date().Bind(temporal.Date{}); err == nil {
		t.Fatal("absent date encoded as a real date")
	}
	if _, err := codec.WallTime().Bind(temporal.Time{}); err != nil {
		t.Fatal("zero wall time should be midnight")
	}
}
