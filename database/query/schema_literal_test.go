package query

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestSchemaLiteralRepresentationAndBounds(t *testing.T) {
	for _, test := range []struct {
		value any
		sql   string
	}{
		{nil, "NULL"}, {true, "TRUE"}, {false, "FALSE"}, {int64(-12), "-12"},
		{float64(1.25), "1.25"}, {math.Copysign(0, -1), "0"},
		{"a'\\\nκ", "E'a''\\\\\nκ'"},
		{[]byte{0, 255}, "DECODE('00ff', 'hex')"}, {[]byte{}, "DECODE('', 'hex')"},
		{[]byte(nil), "NULL"},
		{time.Date(2026, 9, 12, 15, 1, 2, 123456000, time.UTC), "E'2026-09-12T15:01:02.123456Z'"},
	} {
		var c compiler
		got, err := c.schemaLiteral(test.value)
		if err != nil || got != test.sql || len(c.arguments) != 0 || c.scalarSQLBytes != len(got) {
			t.Fatal(got, err)
		}
	}
	for _, input := range []any{
		math.NaN(), math.Inf(1), struct{}{}, "nul\x00", string([]byte{255}),
		strings.Repeat("'", MaxScalarSQLBytes/2), make([]byte, MaxScalarSQLBytes/2),
	} {
		var c compiler
		if _, err := c.schemaLiteral(input); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid or unbounded schema literal accepted", err)
		}
	}
	c := compiler{scalarSQLBytes: MaxScalarSQLBytes - 3}
	if _, err := c.schemaLiteral(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("constant bypassed shared SQL budget", err)
	}
}
