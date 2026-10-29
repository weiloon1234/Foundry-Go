package keyqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/keyqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func buckets() rowset.Query[models.User, keyqueries.BucketCount] {
	u := models.Users
	bucket := expr.When(expr.ModelPredicate(orm.NumberGt(u.Age, 20)), expr.Param(orm.NumberExpr(u.Age), 1)).Else(expr.Param(orm.NumberExpr(u.Age), 0))
	return rowset.OrderBy(rowset.GroupBy(rowset.Select(rowset.From(models.Users.Query()), keyqueries.BucketCounts.Row,
		rowset.MapSelected(keyqueries.BucketCounts.Bucket, bucket.Value()),
		rowset.MapSelected(keyqueries.BucketCounts.Count, expr.Count[models.User]().Value()),
	), expr.Group(bucket)), bucket.Asc())
}

func TestComputedKeyContracts(t *testing.T) {
	u := models.Users
	base := models.Users.Query()
	bucket := expr.When(expr.ModelPredicate(orm.NumberGt(u.Age, 20)), expr.Param(orm.NumberExpr(u.Age), 1)).Else(expr.Param(orm.NumberExpr(u.Age), 0))
	selected := bucket.Value()
	window := expr.WindowFor(rowset.ModelScope(base)).PartitionByValues(selected.Key()).OrderBy(expr.Value(orm.NumberExpr(u.Age)).Asc())
	if _, err := rowset.CompileRead(rowset.ReadScalar(rowset.SelectScalar(rowset.From(base), expr.RowNumber(window)))); err != nil {
		t.Fatal(err)
	}
	if _, err := rowset.CompileRead(rowset.Read(rowset.OrderBy(rowset.DistinctOnValues(rowset.FromModel(base), selected.Key()), bucket.Asc(), expr.Value(orm.NumberExpr(u.Age)).Asc()))); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []rowset.Scalar[models.User, int]{
		rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), expr.Row(expr.Value(orm.NumberExpr(u.Age))).Value()), expr.Group(bucket)),
		rowset.GroupByScalar(rowset.SelectScalar(rowset.From(base), bucket.Value()), expr.Group(bucket), expr.Group(bucket)),
		rowset.OrderScalar(rowset.DistinctOnScalar(rowset.SelectScalar(rowset.From(base), bucket.Value()), expr.Group(bucket)), expr.Value(orm.NumberExpr(u.Age)).Asc()),
		rowset.DistinctOnScalarValues(rowset.SelectScalar(rowset.From(base), bucket.Value())),
	} {
		if _, err := rowset.ReadScalar(invalid).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid key reached execution", err)
		}
	}
	count := expr.Count[models.User]()
	invalidWindow := expr.WindowFor(rowset.ModelScope(base)).PartitionByValues(expr.RowNumber(expr.WindowFor(rowset.ModelScope(base))).Key())
	if _, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(base), expr.Over(count, invalidWindow))).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested window partition reached execution", err)
	}
	testComputedCursorKey(t)
}
