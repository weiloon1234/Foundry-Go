package jsonqueries_test

import (
	"encoding/json"
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"testing"
)

func TestSnapshotJSONKindIdentityAndSQLParity(t *testing.T) {
	if reflect.TypeFor[query.JSONKind]() != reflect.TypeFor[expr.JSONKind]() || reflect.TypeFor[query.JSONKind]() != reflect.TypeFor[database.JSONKind]() {
		t.Fatal("split JSONKind identity")
	}
	f, r := jsonqueries.Documents, jsonqueries.Snapshots
	filter := document(t, jsonqueries.Preferences{Theme: value.Set("dark")})
	selected := rows.Select(rows.From(f.Query().OrderBy(f.Key().Asc())), r.Row,
		rows.Map(r.ID, expr.Value(f.Key())), rows.Map(r.Settings, expr.Value(f.Settings)),
		rows.MapSelected(r.Kind, expr.JSONType(expr.Value(f.Settings)).Value()),
		rows.MapSelected(r.BackupKind, expr.JSONTypeNullable(expr.Value(f.Backup)).Value()),
		rows.MapSelected(r.Matches, expr.JSONContainsNullable(expr.Value(f.Backup), expr.NullableRow(expr.Param(f.Settings, filter))).Value()))
	got, err := rows.CompileRead(rows.Read(selected))
	if err != nil {
		t.Fatal(err)
	}
	old := jsonqueries.DocumentFields()
	legacy := jsonqueries.ProjectSnapshot(jsonqueries.QueryJsonDocuments().OrderBy(old.ID.Asc())).SelectID(query.Value(old.ID)).SelectSettings(query.Value(old.Settings)).SelectKind(old.Settings.Kind().Value()).SelectBackupKind(old.Backup.Kind().Value()).SelectMatches(query.JSONContainsNullable(old.Backup, query.NullableRow(query.Param(old.Settings, filter))).Value()).Query()
	want, err := legacy.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("Snapshot parity\n%s\n%s", got.SQL(), want.SQL())
	}
	before := jsonqueries.Snapshot{ID: 1, Settings: filter, Kind: query.JSONObject, BackupKind: value.Of(query.JSONNull), Matches: value.Of(true)}
	after := jsonqueries.Snapshot{ID: 1, Settings: filter, Kind: expr.JSONObject, BackupKind: value.Of(expr.JSONNull), Matches: value.Of(true)}
	oldBytes, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	newBytes, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(oldBytes) != string(newBytes) || string(newBytes) != `{"ID":1,"Settings":{"theme":"dark"},"Kind":"object","BackupKind":"null","Matches":true}` {
		t.Fatalf("Snapshot wire bytes changed: %s", newBytes)
	}
}
