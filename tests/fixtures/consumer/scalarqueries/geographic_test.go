package scalarqueries_test

import (
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"testing"
)

func TestGeographicExpressionFromConsumer(t *testing.T) {
	f := scalarqueries.Samples
	origin, err := expr.NewGeographicPoint(3.14, 101.68)
	if err != nil {
		t.Fatal(err)
	}
	distance := expr.SphericalDistance(expr.Value(f.Amount), expr.Value(f.Amount), origin)
	if _, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(expr.OrderBy(expr.Where(scalarqueries.Samples.Query(), distance.Lte(5000)), distance.Asc(), expr.Value(f.ID).Asc())), distance.Value()))); err != nil {
		t.Fatal(err)
	}
}
