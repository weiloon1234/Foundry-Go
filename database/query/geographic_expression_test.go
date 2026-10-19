package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/decimal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"math"
	"strings"
	"testing"
)

func TestGeographicPointAndTypedDistance(t *testing.T) {
	for _, pair := range [][2]float64{{91, 0}, {-91, 0}, {0, 181}, {0, -181}, {math.NaN(), 0}, {0, math.Inf(1)}} {
		if _, err := NewGeographicPoint(pair[0], pair[1]); err == nil {
			t.Fatal("invalid point accepted", pair)
		}
	}
	origin, _ := NewGeographicPoint(3.14, 101.68)
	lat := NewExactField[cursorRecord, decimal.Decimal]("records", "id", codec.Decimal())
	distance := SphericalDistance(lat, lat, origin)
	statement, err := SelectValue(cursorQuery().Where(distance.Lte(500)).OrderBy(distance.Asc()), distance.Value()).Compile()
	if err != nil || !strings.Contains(statement.SQL(), "ASIN") || strings.Contains(statement.SQL(), "101.68") {
		t.Fatal(statement.SQL(), err)
	}
	if _, err := SelectValue(cursorQuery(), SphericalDistance[cursorRecord, decimal.Decimal](nil, lat, origin).Value()).Compile(); err == nil {
		t.Fatal("undeclared coordinate accepted")
	}
}

func TestPostgresSphericalDistance(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`SET LOCAL search_path TO "` + namespace + `"`, `CREATE TABLE records (id bigint PRIMARY KEY, rank bigint)`, `INSERT INTO records (id) VALUES (1)`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		f := NewOrderedField[cursorRecord, float64]("records", "id", codec.Float[float64]())
		origin := GeographicPoint{}
		for _, tc := range []struct{ lat, lon, want, tolerance float64 }{{0, 0, 0, 0.001}, {0, 1, 111195.0802335, 0.01}, {0, 180, math.Pi * 6371008.8, 0.1}, {90, 180, math.Pi * 6371008.8 / 2, 0.1}, {0, -179.9, 179.9 * math.Pi / 180 * 6371008.8, 0.1}} {
			got, err := SelectValue(cursorQuery(), SphericalDistance(Param(f, tc.lat), Param(f, tc.lon), origin).Value()).RequireFirst(t.Context(), tx)
			if err != nil {
				return err
			}
			if math.Abs(got-tc.want) > tc.tolerance {
				t.Fatalf("distance %v,%v = %v, want %v", tc.lat, tc.lon, got, tc.want)
			}
		}
		nullable := SphericalDistanceNullable(NullFor(f), NullableRow(Param(f, 1)), origin)
		got, err := SelectValue(cursorQuery(), nullable.Value()).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		if !got.IsNull() {
			t.Fatal("NULL coordinates became a distance", got)
		}
		crossing, _ := NewGeographicPoint(0, 179.9)
		actual, err := SelectValue(cursorQuery(), SphericalDistance(Param(f, 0), Param(f, -179.9), crossing).Value()).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		if math.Abs(actual-22239.016) > 0.1 {
			t.Fatal("antimeridian", actual)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
