package jsonqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestComputedRowComparisonFilters(t *testing.T) {
	f := jsonqueries.Documents
	wanted := expr.Param(orm.JSONExpr(f.Settings), document(t, jsonqueries.Preferences{Theme: value.Set("dark")}))
	matches := expr.CompareFilter(expr.JSONContains(expr.Value(orm.JSONExpr(f.Settings)), wanted))
	backup := expr.NullableCompareFilter(expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(f.Backup)), expr.NullableRow(wanted)))
	length := expr.CompareFilter(expr.Add(expr.Value(orm.KeyExpr(f.Key())), expr.Param(orm.KeyExpr(f.Key()), 1)))

	var _ datatable.RowEquality[jsonqueries.Document, bool] = matches
	var _ datatable.RowEquality[jsonqueries.Document, int] = length
	var _ datatable.RowNullable[jsonqueries.Document, bool] = backup

	boolFilter := datatable.Where(foundryhttp.BoolQuery[bool](), matches)
	scalarFilter := datatable.Where(foundryhttp.IntegerQuery[int](), length)
	nullableFilter := datatable.NullableWhere(foundryhttp.BoolQuery[bool](), backup)
	var _ datatable.FilterRegistration[jsonqueries.Document] = datatable.DefineFilter("matches", "documents.matches", boolFilter)
	var _ datatable.FilterRegistration[jsonqueries.Document] = datatable.DefineFilter("nextID", "documents.next_id", scalarFilter)
	var _ datatable.FilterRegistration[jsonqueries.Document] = datatable.DefineFilter("backupMatches", "documents.backup_matches", nullableFilter)
	for _, test := range []struct {
		describe func() (datatable.FilterInfo, error)
		nullable bool
	}{{boolFilter.Description, false}, {scalarFilter.Description, false}, {nullableFilter.Description, true}} {
		info, err := test.describe()
		want := []datatable.Operator{datatable.Equal, datatable.In, datatable.NotEqual, datatable.NotIn}
		if test.nullable {
			want = []datatable.Operator{datatable.Equal, datatable.In, datatable.IsNotNull, datatable.IsNull, datatable.NotEqual, datatable.NotIn}
		}
		if err != nil || info.Phase != datatable.WherePhase || info.Nullable != test.nullable || !reflect.DeepEqual(info.Operators, want) {
			t.Fatal("computed equality filter changed capabilities", info, err)
		}
	}

	const (
		settingsSQL = `(CAST("json_documents"."settings" AS jsonb) @> CAST(CAST($1 AS jsonb) AS jsonb))`
		backupSQL   = `(CAST("json_documents"."backup" AS jsonb) @> CAST(CAST($1 AS jsonb) AS jsonb))`
		nextSQL     = `(CAST("json_documents"."id" AS bigint) + CAST(CAST($1 AS bigint) AS bigint))`
	)

	for _, test := range []struct {
		name      string
		predicate expr.Predicate[jsonqueries.Document]
		where     string
		args      []any
	}{
		{"boolean eq false", matches.Eq(false), settingsSQL + ` = $2`, []any{`{"theme":"dark"}`, false}},
		{"boolean ne", matches.Ne(true), settingsSQL + ` <> $2`, []any{`{"theme":"dark"}`, true}},
		{"boolean in", matches.In(false, true), settingsSQL + ` IN ($2, $3)`, []any{`{"theme":"dark"}`, false, true}},
		{"scalar eq zero", length.Eq(0), nextSQL + ` = $2`, []any{int64(1), int64(0)}},
		{"scalar ne", length.Ne(4), nextSQL + ` <> $2`, []any{int64(1), int64(4)}},
		{"scalar in", length.In(2, 4), nextSQL + ` IN ($2, $3)`, []any{int64(1), int64(2), int64(4)}},
		{"nullable eq false", backup.Eq(false), backupSQL + ` = $2`, []any{`{"theme":"dark"}`, false}},
		{"nullable ne", backup.Ne(true), backupSQL + ` <> $2`, []any{`{"theme":"dark"}`, true}},
		{"nullable in", backup.In(false, true), backupSQL + ` IN ($2, $3)`, []any{`{"theme":"dark"}`, false, true}},
		{"nullable null", backup.IsNull(), backupSQL + ` IS NULL`, []any{`{"theme":"dark"}`}},
		{"nullable present", backup.IsNotNull(), backupSQL + ` IS NOT NULL`, []any{`{"theme":"dark"}`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			statement, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(expr.Where(f.Query(), test.predicate)), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(f.Key())))))))
			if err != nil {
				t.Fatal(err)
			}
			if statement.SQL() != `SELECT "json_documents"."id" AS "value" FROM "json_documents" WHERE (`+test.where+`)` || !reflect.DeepEqual(statement.Arguments(), test.args) {
				t.Fatalf("SQL: %s\narguments: %#v\nwant WHERE %s\narguments: %#v", statement.SQL(), statement.Arguments(), test.where, test.args)
			}
		})
	}
}
