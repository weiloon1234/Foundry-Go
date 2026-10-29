package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"github.com/weiloon1234/Foundry-Go/value"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"math"
	"strings"
	"testing"
)

func TestGeographicPointAndTypedDistance(t *testing.T) {
	for _, pair := range [][2]float64{{91, 0}, {-91, 0}, {0, 181}, {0, -181}, {math.NaN(), 0}, {0, math.Inf(1)}} {
		if _, err := expr.NewGeographicPoint(pair[0], pair[1]); err == nil {
			t.Fatal("invalid point accepted", pair)
		}
	}
	d := declareAggregateRecords()
	origin, _ := expr.NewGeographicPoint(3.14, 101.68)
	lat := expr.Value(orm.NumberExpr(d.Exact))
	distance := expr.SphericalDistance(lat, lat, origin)
	q := rows.SelectScalar(rows.From(expr.OrderBy(expr.Where(d.Query(), distance.Lte(500)), distance.Asc())), distance.Value())
	s, err := rows.CompileRead(rows.ReadScalar(q))
	if err != nil || !strings.Contains(s.SQL(), "ASIN") || strings.Contains(s.SQL(), "101.68") {
		t.Fatal(s.SQL(), err)
	}
	bad := expr.SphericalDistance(expr.RowExpression[aggregateRecord, int64]{}, lat, origin)
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.FromModel(d.Query()), bad.Value()))); err == nil {
		t.Fatal("undeclared coordinate accepted")
	}
}
func TestPostgresSphericalDistance(t *testing.T) {
	db := pgtest.Open(t)
	namespace := pgtest.Namespace(t, db)
	type point struct {
		ID       int64
		Latitude float64
	}
	type table struct {
		orm.Table[point, int64]
		Latitude orm.Number[point, float64]
	}
	d := orm.Define(namespace+".geographic_points", func(b orm.Builder[point], m *point) table {
		return table{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Latitude)}
	})
	err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, sql := range []string{`CREATE TABLE "` + namespace + `".geographic_points (id bigint PRIMARY KEY, latitude double precision)`, `INSERT INTO "` + namespace + `".geographic_points (id,latitude) VALUES (1,0)`} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		f := expr.Value(orm.NumberExpr(d.Latitude))
		source := rows.FromModel(d.Query())
		origin := expr.GeographicPoint{}
		for _, tc := range []struct{ lat, lon, want, tolerance float64 }{{0, 0, 0, 0.001}, {0, 1, 111195.0802335, 0.01}, {0, 180, math.Pi * 6371008.8, 0.1}, {90, 180, math.Pi * 6371008.8 / 2, 0.1}, {0, -179.9, 179.9 * math.Pi / 180 * 6371008.8, 0.1}} {
			got, err := rows.ReadScalar(rows.SelectScalar(source, expr.SphericalDistance(expr.Param(orm.NumberExpr(d.Latitude), tc.lat), expr.Param(orm.NumberExpr(d.Latitude), tc.lon), origin).Value())).RequireFirst(t.Context(), tx)
			if err != nil {
				return err
			}
			if math.Abs(got-tc.want) > tc.tolerance {
				t.Fatalf("distance %v,%v = %v, want %v", tc.lat, tc.lon, got, tc.want)
			}
		}
		nullable := expr.SphericalDistanceNullable(expr.NullableRow(f).Param(value.Null[float64]()), expr.NullableRow(expr.Param(orm.NumberExpr(d.Latitude), 1)), origin)
		got, err := rows.ReadScalar(rows.SelectScalar(source, nullable.Value())).RequireFirst(t.Context(), tx)
		if err != nil {
			return err
		}
		if !got.IsNull() {
			t.Fatal("NULL coordinates became distance")
		}
		crossing, _ := expr.NewGeographicPoint(0, 179.9)
		actual, err := rows.ReadScalar(rows.SelectScalar(source, expr.SphericalDistance(expr.Param(orm.NumberExpr(d.Latitude), 0), expr.Param(orm.NumberExpr(d.Latitude), -179.9), crossing).Value())).RequireFirst(t.Context(), tx)
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
