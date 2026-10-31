package jsonqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
	"testing"
)

func checkSnapshotKind(t *testing.T, tx *database.Tx, original []jsonqueries.Document) error {
	f, r := jsonqueries.Documents, jsonqueries.Snapshots
	filter := document(t, jsonqueries.Preferences{Theme: value.Set("dark")})
	selected := rowset.Select(rowset.From(f.Query().OrderBy(orm.KeyAsc(f.Key()))), r.Row,
		rowset.Map(r.ID, expr.Value(orm.KeyExpr(f.Key()))), rowset.Map(r.Settings, expr.Value(orm.JSONExpr(f.Settings))),
		rowset.MapSelected(r.Kind, expr.AsSelected(expr.JSONType(expr.Value(orm.JSONExpr(f.Settings))))),
		rowset.MapSelected(r.BackupKind, expr.AsSelected(expr.JSONTypeNullable(expr.Value(orm.NullableJSONExpr(f.Backup))))),
		rowset.MapSelected(r.Matches, expr.AsSelected(expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(f.Backup)), expr.NullableRow(expr.Param(orm.JSONExpr(f.Settings), filter))))))
	rows, err := rowset.Read(selected).All(t.Context(), tx)
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	for i, row := range rows {
		if row.Settings != original[i].Settings || row.Kind != database.JSONObject {
			t.Fatal(row)
		}
		if i < 2 && (!row.BackupKind.IsNull() || !row.Matches.IsNull()) {
			t.Fatal("SQL NULL lost", row)
		}
	}
	if kind, ok := rows[2].BackupKind.Get(); !ok || kind != database.JSONObject {
		t.Fatal(rows[2])
	}
	if matched, ok := rows[2].Matches.Get(); !ok || !matched {
		t.Fatal(rows[2])
	}
	return nil
}
