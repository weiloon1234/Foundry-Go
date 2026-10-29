package keyqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/keyqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
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

	if !oracleEqual(t, "consumer/keyqueries/cursor_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}

	values := rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(models.Users.Query().Limit(5)), expr.Row(expr.Value(orm.NullableTextExpr(models.Users.Nickname))).Value())))
	valueCursor := values.OrderBy(values.Value().Desc()).UniqueBy(expr.Group(values.Value()))
	scalar, err := rows.CompileValueCursor(valueCursor)
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.PreflightValueCursor(t.Context(), valueCursor); err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/keyqueries/cursor_parity_test/scalar", oracle.Snapshot([]any{scalar.SQL(), scalar.Arguments()})) {
		t.Fatal("recorded scalar cursor parity changed")
	}
}
