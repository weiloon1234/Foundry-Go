package jsonqueries_test

import (
	"encoding/json"
	"foundry.test/consumer/jsonqueries"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"

	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"github.com/weiloon1234/Foundry-Go/value"

	"testing"
)

func TestSnapshotJSONKindIdentityAndSQLParity(t *testing.T) {
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

	if !oracleEqual(t, "consumer/jsonqueries/snapshot_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("recorded SQL parity changed")
	}

	after := jsonqueries.Snapshot{ID: 1, Settings: filter, Kind: expr.JSONObject, BackupKind: value.Of(expr.JSONNull), Matches: value.Of(true)}

	newBytes, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "consumer/jsonqueries/snapshot_parity_test/wire", oracle.Snapshot(newBytes)) {
		t.Fatal("recorded JSON wire parity changed")
	}
	if string(newBytes) != `{"ID":1,"Settings":{"theme":"dark"},"Kind":"object","BackupKind":"null","Matches":true}` {
		t.Fatalf("Snapshot wire bytes changed: %s", newBytes)
	}
}
