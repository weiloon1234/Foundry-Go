package sqlvalue_test

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
)

func TestSQLValueKeepsExistingCursorWireAndExactValues(t *testing.T) {
	instant := time.Date(2026, 9, 13, 12, 34, 56, 123456789, time.FixedZone("offset", 8*60*60))
	cases := []struct {
		input    driver.Value
		wire     string
		expected driver.Value
	}{
		{nil, `{"k":"null"}`, nil},
		{[]byte(nil), `{"k":"null"}`, nil},
		{[]byte{}, `{"k":"bytes"}`, []byte{}},
		{"", `{"k":"string"}`, ""},
		{" exact ", `{"k":"string","t":" exact "}`, " exact "},
		{[]byte{0, 255}, `{"k":"bytes","t":"AP8"}`, []byte{0, 255}},
		{int64(math.MaxInt64), `{"k":"int","t":"9223372036854775807"}`, int64(math.MaxInt64)},
		{1.25, `{"k":"float","t":"1.25"}`, 1.25},
		{false, `{"k":"bool","t":"false"}`, false},
		{instant, `{"k":"time","t":"2026-09-13T04:34:56.123456789Z"}`, instant.UTC()},
	}
	for _, tc := range cases {
		encoded, err := sqlvalue.Encode(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(encoded)
		if err != nil || string(wire) != tc.wire {
			t.Fatal("existing SQL value wire changed", string(wire), err)
		}
		decoded, err := encoded.Decode()
		if err != nil || !reflect.DeepEqual(decoded, tc.expected) {
			t.Fatal("SQL value did not round trip exactly", err)
		}
	}
	input := []byte{0, 255}
	snapshot, err := sqlvalue.Encode(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 99
	first, err := snapshot.Decode()
	if err != nil {
		t.Fatal(err)
	}
	first.([]byte)[0] = 42
	second, err := snapshot.Decode()
	if err != nil || !reflect.DeepEqual(second, []byte{0, 255}) {
		t.Fatal("SQL snapshot shares byte storage", err)
	}
}
func TestSQLValueRejectsInvalidKindsAndValues(t *testing.T) {
	for _, input := range []driver.Value{math.NaN(), math.Inf(1), int(7), "\xff", struct{}{}} {
		if _, err := sqlvalue.Encode(input); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid SQL source accepted", err)
		}
	}
	for _, input := range []sqlvalue.Value{
		{}, {Kind: "null", Text: "value"}, {Kind: "bytes", Text: "AB"}, {Kind: "int", Text: "9223372036854775808"},
		{Kind: "float", Text: "Inf"}, {Kind: "bool", Text: "1"}, {Kind: "time", Text: "yesterday"},
	} {
		if _, err := input.Decode(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid encoded SQL value accepted", input, err)
		}
	}
}
