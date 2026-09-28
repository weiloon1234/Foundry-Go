package query

import (
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresSchemaLiteralRoundTrips(t *testing.T) {
	db := pgtest.Open(t)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, mode := range []string{"on", "off"} {
			if _, err := tx.Exec(t.Context(), "SET LOCAL standard_conforming_strings = "+mode); err != nil {
				return err
			}
			for _, test := range []struct {
				value any
				kind  string
			}{
				{nil, "text"}, {true, "boolean"}, {false, "boolean"},
				{int64(math.MinInt64), "bigint"}, {int64(math.MaxInt64), "bigint"},
				{float64(1.25), "double precision"}, {math.SmallestNonzeroFloat64, "double precision"},
				{math.MaxFloat64, "double precision"}, {math.Copysign(0, -1), "double precision"},
				{"'\\\n世界; --", "text"}, {[]byte{0, 255, 39, 92}, "bytea"},
				{[]byte{}, "bytea"}, {[]byte(nil), "bytea"},
				{time.Date(2026, 9, 12, 15, 1, 2, 123456000, time.UTC), "timestamptz"},
				{time.Date(1900, 1, 2, 3, 4, 5, 123456000, time.FixedZone("seconds", 8*3600+21)), "timestamptz"},
			} {
				var c compiler
				literal, err := c.schemaLiteral(test.value)
				if err != nil {
					return err
				}
				var equal bool
				err = database.ScanOne(t.Context(), tx, "SELECT CAST("+literal+" AS "+test.kind+") IS NOT DISTINCT FROM CAST($1 AS "+test.kind+")", []any{test.value}, &equal)
				if err != nil {
					return err
				}
				if !equal {
					t.Fatalf("schema literal changed %T with string mode %s", test.value, mode)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
