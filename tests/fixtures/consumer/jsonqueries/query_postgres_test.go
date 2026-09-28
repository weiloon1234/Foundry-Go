package jsonqueries_test

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresJSONPredicatesAndProjections(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.QueryJsonDocuments(), jsonqueries.DocumentFields()
		filter := document(t, jsonqueries.Preferences{Theme: value.Set("dark")})
		for _, test := range []struct {
			predicate query.Predicate[jsonqueries.Document]
			count     int64
		}{
			{f.Settings.Eq(filter), 2}, {f.Settings.Contains(filter), 2},
			{f.Settings.ContainedBy(filter), 3}, {f.Settings.Kind().Eq(query.JSONObject), 3},
			{f.Tags.Contains(document(t, []string{"go"})), 2},
			{f.Tags.Eq(document(t, []string{"db", "go"})), 1},
			{f.Backup.IsNull(), 2}, {f.Backup.Contains(filter), 1},
			{f.Backup.ContainedBy(filter), 1}, {f.State.IsJSONNull(), 2},
		} {
			count, err := q.Where(test.predicate).Count(t.Context(), tx)
			if err != nil || count != test.count {
				t.Fatal("unexpected JSON filter count", count, test.count, err)
			}
		}
		selected := jsonqueries.ProjectSnapshot(q.OrderBy(f.ID.Asc())).SelectID(f.ID.Value()).SelectSettings(f.Settings.Value()).SelectKind(f.Settings.Kind().Value()).SelectBackupKind(f.Backup.Kind().Value()).SelectMatches(query.JSONContainsNullable(f.Backup, query.NullableRow(f.Settings.Param(filter))).Value())
		rows, err := selected.Query().All(t.Context(), tx)
		if err != nil || len(rows) != 3 {
			t.Fatal(rows, err)
		}
		for i, row := range rows {
			if row.Settings != original[i].Settings || row.Kind != query.JSONObject {
				t.Fatal(row)
			}
			if i < 2 && (!row.BackupKind.IsNull() || !row.Matches.IsNull()) {
				t.Fatal("SQL NULL lost", row)
			}
		}
		if kind, ok := rows[2].BackupKind.Get(); !ok || kind != query.JSONObject {
			t.Fatal(rows[2])
		}
		if matched, ok := rows[2].Matches.Get(); !ok || !matched {
			t.Fatal(rows[2])
		}
		grouped := query.SelectValue(q, f.Settings.Kind().Value()).GroupBy(f.Settings.Group()).Having(query.CompareValue(query.JSONContainsValue(f.Settings.Value(), f.Settings.Param(filter).Value())).Eq(true))
		if kinds, err := grouped.All(t.Context(), tx); err != nil || len(kinds) != 1 || kinds[0] != query.JSONObject {
			t.Fatal(kinds, err)
		}
		return nil
	})
}

func TestPostgresJSONWritesRelationsAndNaturalKeys(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.QueryJsonDocuments(), jsonqueries.DocumentFields()
		loaded, err := q.OrderBy(f.ID.Asc()).With(jsonqueries.DocumentRelations().Policy).All(t.Context(), tx)
		if err != nil || len(loaded) != 3 {
			t.Fatal(loaded, err)
		}
		for _, row := range loaded {
			optional, wasLoaded := row.Policy.Get()
			policy, present := optional.Get()
			if !wasLoaded || !present || policy.Name != "primary" {
				t.Fatal(policy, wasLoaded, present)
			}
		}
		same, err := value.ParseJSON[jsonqueries.PolicyKey](`{"region":"apac","version":9007199254740993.000}`)
		if err != nil {
			return err
		}
		policy, err := jsonqueries.QueryJsonPolicies().RequireFind(t.Context(), tx, same)
		if err != nil || policy.ID != original[0].PolicyID {
			t.Fatal(policy, err)
		}
		updated, err := q.Update(t.Context(), tx, 1, jsonqueries.DocumentDraft{}.SetBackup(original[0].Settings))
		if err != nil || updated.Backup.IsNull() || updated.Settings != original[0].Settings {
			t.Fatal(updated, err)
		}
		cleared, err := q.Update(t.Context(), tx, 1, jsonqueries.DocumentDraft{}.ClearBackup())
		if err != nil || !cleared.Backup.IsNull() || cleared.Settings != original[0].Settings {
			t.Fatal(cleared, err)
		}
		if _, err := q.Update(t.Context(), tx, 1, jsonqueries.DocumentDraft{}.SetSettings(value.JSON[jsonqueries.Preferences]{})); err == nil {
			t.Fatal("zero JSON wrote")
		}
		kept, err := q.RequireFind(t.Context(), tx, 1)
		if err != nil || kept.Settings != original[0].Settings {
			t.Fatal("failed write changed row", err)
		}
		price, err := decimal.Parse("9007199254740993.123456789")
		if err != nil {
			return err
		}
		prefs := document(t, jsonqueries.Preferences{Quota: value.Set(price), Labels: value.Set(map[string]string{"lang": "日本語"})})
		withQuota, err := q.Update(t.Context(), tx, 1, jsonqueries.DocumentDraft{}.SetSettings(prefs))
		if err != nil || withQuota.Settings != prefs {
			t.Fatal(withQuota, err)
		}
		return nil
	})
}
