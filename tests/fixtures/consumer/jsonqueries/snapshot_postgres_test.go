package jsonqueries_test

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
	selected := rowset.Select(rowset.From(f.Query().OrderBy(f.Key().Asc())), r.Row,
		rowset.Map(r.ID, expr.Value(f.Key())), rowset.Map(r.Settings, expr.Value(f.Settings)),
		rowset.MapSelected(r.Kind, expr.JSONType(expr.Value(f.Settings)).Value()),
		rowset.MapSelected(r.BackupKind, expr.JSONTypeNullable(expr.Value(f.Backup)).Value()),
		rowset.MapSelected(r.Matches, expr.JSONContainsNullable(expr.Value(f.Backup), expr.NullableRow(expr.Param(f.Settings, filter))).Value()))
	rows, err := selected.All(t.Context(), tx)
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	for i, row := range rows {
		if row.Settings != original[i].Settings || row.Kind != expr.JSONObject {
			t.Fatal(row)
		}
		if i < 2 && (!row.BackupKind.IsNull() || !row.Matches.IsNull()) {
			t.Fatal("SQL NULL lost", row)
		}
	}
	if kind, ok := rows[2].BackupKind.Get(); !ok || kind != expr.JSONObject {
		t.Fatal(rows[2])
	}
	if matched, ok := rows[2].Matches.Get(); !ok || !matched {
		t.Fatal(rows[2])
	}
	return nil
}
