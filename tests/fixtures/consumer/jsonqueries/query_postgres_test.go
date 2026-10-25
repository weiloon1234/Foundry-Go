package jsonqueries_test

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresJSONPredicatesAndProjections(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
		filter := document(t, jsonqueries.Preferences{Theme: value.Set("dark")})
		for _, test := range []struct {
			predicate expr.Predicate[jsonqueries.Document]
			count     int64
		}{
			{expr.ModelPredicate(f.Settings.Eq(filter)), 2}, {expr.JSONContains(expr.Value(f.Settings), expr.Param(f.Settings, filter)).Eq(true), 2},
			{expr.JSONContainedBy(expr.Value(f.Settings), expr.Param(f.Settings, filter)).Eq(true), 3}, {expr.JSONType(expr.Value(f.Settings)).Eq(expr.JSONObject), 3},
			{expr.JSONContains(expr.Value(f.Tags), expr.Param(f.Tags, document(t, []string{"go"}))).Eq(true), 2},
			{expr.ModelPredicate(f.Tags.Eq(document(t, []string{"db", "go"}))), 1},
			{expr.ModelPredicate(f.Backup.IsNull()), 2}, {expr.JSONContainsNullable(expr.Value(f.Backup), expr.NullableRow(expr.Param(f.Settings, filter))).Eq(value.Of(true)), 1},
			{expr.JSONContainedByNullable(expr.Value(f.Backup), expr.NullableRow(expr.Param(f.Settings, filter))).Eq(value.Of(true)), 1}, {expr.JSONRoot(expr.Value(f.State)).IsJSONNull(), 2},
		} {
			count, err := expr.Where(q, test.predicate).Count(t.Context(), tx)
			if err != nil || count != test.count {
				t.Fatal("unexpected JSON filter count", count, test.count, err)
			}
		}
		if err := checkSnapshotKind(t, tx, original); err != nil {
			return err
		}
		grouped := rowset.HavingScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), expr.JSONType(expr.Value(f.Settings)).Value()), expr.Group(expr.Value(f.Settings))), expr.CompareValue(expr.JSONContainsValue(expr.Select(expr.Value(f.Settings)), expr.Param(f.Settings, filter).Value())).Eq(true))
		if kinds, err := rowset.ReadScalar(grouped).All(t.Context(), tx); err != nil || len(kinds) != 1 || kinds[0] != expr.JSONObject {
			t.Fatal(kinds, err)
		}
		return nil
	})
}

func TestPostgresJSONWritesRelationsAndNaturalKeys(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
		loaded, err := q.OrderBy(f.Key().Asc()).With(jsonqueries.DocumentLinks.Policy).All(t.Context(), tx)
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
		policy, err := jsonqueries.Policies.Require(t.Context(), tx, same)
		if err != nil || policy.ID != original[0].PolicyID {
			t.Fatal(policy, err)
		}
		updated, err := orm.Update(t.Context(), tx, q, f.Key(), 1, f.Backup.Set(original[0].Settings))
		if err != nil || updated.Backup.IsNull() || updated.Settings != original[0].Settings {
			t.Fatal(updated, err)
		}
		cleared, err := orm.Update(t.Context(), tx, q, f.Key(), 1, f.Backup.SetNull())
		if err != nil || !cleared.Backup.IsNull() || cleared.Settings != original[0].Settings {
			t.Fatal(cleared, err)
		}
		if _, err := orm.Update(t.Context(), tx, q, f.Key(), 1, f.Settings.Set(value.JSON[jsonqueries.Preferences]{})); err == nil {
			t.Fatal("zero JSON wrote")
		}
		kept, err := jsonqueries.Documents.Require(t.Context(), tx, 1)
		if err != nil || kept.Settings != original[0].Settings {
			t.Fatal("failed write changed row", err)
		}
		price, err := decimal.Parse("9007199254740993.123456789")
		if err != nil {
			return err
		}
		prefs := document(t, jsonqueries.Preferences{Quota: value.Set(price), Labels: value.Set(map[string]string{"lang": "日本語"})})
		withQuota, err := orm.Update(t.Context(), tx, q, f.Key(), 1, f.Settings.Set(prefs))
		if err != nil || withQuota.Settings != prefs {
			t.Fatal(withQuota, err)
		}
		return nil
	})
}
