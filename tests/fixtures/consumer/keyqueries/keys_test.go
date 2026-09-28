package keyqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/keyqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func buckets() query.ProjectionQuery[models.User, keyqueries.BucketCount] {
	u := models.UserFields()
	bucket := query.When(u.Age.Gt(20), u.Age.Param(1)).Else(u.Age.Param(0))
	return keyqueries.SelectBucketCount(models.QueryUsers(), keyqueries.BucketCountSelection[models.User]{Bucket: bucket.Value(), Count: query.Count[models.User]().Value()}).GroupBy(bucket.Group()).OrderBy(bucket.Asc())
}

func TestComputedKeyContracts(t *testing.T) {
	u := models.UserFields()
	base := models.QueryUsers()
	bucket := query.When(u.Age.Gt(20), u.Age.Param(1)).Else(u.Age.Param(0))
	selected := bucket.Value()
	window := query.WindowFor(base).PartitionByValues(selected.Key()).OrderBy(u.Age.Asc())
	if _, err := query.SelectValue(base, query.RowNumber(window)).Compile(); err != nil {
		t.Fatal(err)
	}
	if _, err := base.DistinctOnValues(selected.Key()).OrderBy(bucket.Asc(), u.Age.Asc()).Compile(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []query.ValueQuery[models.User, int]{
		query.SelectValue(base, u.Age.Value()).GroupBy(bucket.Group()),
		query.SelectValue(base, bucket.Value()).GroupBy(bucket.Group(), bucket.Group()),
		query.SelectValue(base, bucket.Value()).DistinctOn(bucket.Group()).OrderBy(u.Age.Asc()),
		query.SelectValue(base, bucket.Value()).DistinctOnValues(),
	} {
		if _, err := invalid.All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid key reached execution", err)
		}
	}
	count := query.Count[models.User]()
	invalidWindow := query.WindowFor(base).PartitionByValues(query.RowNumber(query.WindowFor(base)).Key())
	if _, err := query.SelectValue(base, count.Over(invalidWindow)).All(t.Context(), queryfixture.NoQueries(t)); !errors.Is(err, fault.Invalid) {
		t.Fatal("nested window partition reached execution", err)
	}
	// A computed cursor key must first become a declared output field.
	cursor := query.CursorFor(buckets())
	f := keyqueries.BucketCountFieldsAt(cursor.Scope())
	if _, err := cursor.OrderBy(f.Bucket.Asc()).UniqueBy(f.Bucket.Param(1).Group()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("computed cursor key accepted without a declared output field", err)
	}
}
