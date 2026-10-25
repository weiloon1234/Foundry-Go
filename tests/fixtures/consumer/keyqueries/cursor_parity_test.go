package keyqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/keyqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestCursorCompileSQLParity(t *testing.T) {
	cursor := rows.CursorFor(rows.RecordOf(buckets()))
	bucket := rows.At(cursor.Scope(), keyqueries.BucketCounts.Bucket)
	current := cursor.OrderBy(bucket.Desc()).UniqueBy(expr.Group(bucket))
	got, err := rows.CompileCursor(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightCursor(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	u := models.UserFields()
	key := query.When(u.Age.Gt(20), query.Param(u.Age, 1)).Else(query.Param(u.Age, 0))
	oldBuckets := keyqueries.SelectBucketCount(models.QueryUsers(), keyqueries.BucketCountSelection[models.User]{Bucket: key.Value(), Count: query.Count[models.User]().Value()}).GroupBy(key.Group()).OrderBy(key.Asc())
	oldCursor := query.CursorFor(oldBuckets)
	oldBucket := keyqueries.BucketCountFieldsAt(oldCursor.Scope()).Bucket
	want, err := oldCursor.OrderBy(oldBucket.Desc()).UniqueBy(oldBucket.Group()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("cursor compile parity\n%s\n%s", got.SQL(), want.SQL())
	}
	values := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(models.Users.Query().Limit(5)), expr.Select(expr.Value(models.Users.Nickname)))))
	valueCursor := values.OrderBy(values.Value().Desc()).UniqueBy(values.Key())
	scalar, err := rows.CompileValueCursor(valueCursor)
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightValueCursor(t.Context(), valueCursor); err != nil {
		t.Fatal(err)
	}
	oldValues := query.ValueCursorFor(query.SelectValue(models.QueryUsers().Limit(5), query.Value(u.Nickname)))
	oldScalar, err := oldValues.OrderBy(oldValues.Value().Desc()).UniqueBy(oldValues.Key()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if scalar.SQL() != oldScalar.SQL() || !reflect.DeepEqual(scalar.Arguments(), oldScalar.Arguments()) {
		t.Fatalf("scalar cursor compile parity\n%s\n%s", scalar.SQL(), oldScalar.SQL())
	}
}
