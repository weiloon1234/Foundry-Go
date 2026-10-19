package scalarqueries_test

import (
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"testing"
)

func TestGeographicExpressionFromConsumer(t *testing.T) {
	f := scalarqueries.SampleFields()
	origin, err := query.NewGeographicPoint(3.14, 101.68)
	if err != nil {
		t.Fatal(err)
	}
	distance := query.SphericalDistance(f.Amount, f.Amount, origin)
	if _, err := query.SelectValue(scalarqueries.QueryCalculationSamples().Where(distance.Lte(5000)).OrderBy(distance.Asc(), f.ID.Asc()), distance.Value()).Compile(); err != nil {
		t.Fatal(err)
	}
}
