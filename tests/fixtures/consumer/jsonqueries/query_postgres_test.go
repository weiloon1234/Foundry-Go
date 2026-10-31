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
			{expr.ModelPredicate(orm.JSONEq(f.Settings, filter)), 2}, {expr.Eq(expr.JSONContains(expr.Value(orm.JSONExpr(f.Settings)), expr.Param(orm.JSONExpr(f.Settings), filter)), true), 2},
			{expr.Eq(expr.JSONContainedBy(expr.Value(orm.JSONExpr(f.Settings)), expr.Param(orm.JSONExpr(f.Settings), filter)), true), 3}, {expr.Eq(expr.JSONType(expr.Value(orm.JSONExpr(f.Settings))), database.JSONObject), 3},
			{expr.Eq(expr.JSONContains(expr.Value(orm.JSONExpr(f.Tags)), expr.Param(orm.JSONExpr(f.Tags), document(t, []string{"go"}))), true), 2},
			{expr.ModelPredicate(orm.JSONEq(f.Tags, document(t, []string{"db", "go"}))), 1},
			{expr.ModelPredicate(orm.NullableJSONIsNull(f.Backup)), 2}, {expr.Eq(expr.JSONContainsNullable(expr.Value(orm.NullableJSONExpr(f.Backup)), expr.NullableRow(expr.Param(orm.JSONExpr(f.Settings), filter))), value.Of(true)), 1},
			{expr.Eq(expr.JSONContainedByNullable(expr.Value(orm.NullableJSONExpr(f.Backup)), expr.NullableRow(expr.Param(orm.JSONExpr(f.Settings), filter))), value.Of(true)), 1}, {expr.Eq(expr.JSONTypeNullable(expr.JSONRoot(expr.Value(orm.JSONExpr(f.State))).JSON()), value.Of(database.JSONNull)), 2},
		} {
			count, err := orm.Count(t.Context(), tx, expr.Where(q, test.predicate))
			if err != nil || count != test.count {
				t.Fatal("unexpected JSON filter count", count, test.count, err)
			}
		}
		if err := checkSnapshotKind(t, tx, original); err != nil {
			return err
		}
		grouped := rowset.HavingScalar(rowset.GroupByScalar(rowset.SelectScalar(rowset.From(q), expr.AsSelected(expr.JSONType(expr.Value(orm.JSONExpr(f.Settings))))), expr.Group(expr.Value(orm.JSONExpr(f.Settings)))), expr.EqValue(expr.CompareValue(expr.JSONContainsValue(expr.AsSelected(expr.Row(expr.Value(orm.JSONExpr(f.Settings)))), expr.AsSelected(expr.Param(orm.JSONExpr(f.Settings), filter)))), true))
		if kinds, err := rowset.ReadScalar(grouped).All(t.Context(), tx); err != nil || len(kinds) != 1 || kinds[0] != database.JSONObject {
			t.Fatal(kinds, err)
		}
		return nil
	})
}

func TestPostgresJSONWritesRelationsAndNaturalKeys(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
		loaded, err := orm.All(t.Context(), tx, q.OrderBy(orm.KeyAsc(f.Key())).With(jsonqueries.DocumentLinks.Policy))
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
		policy, err := orm.RequireKey(t.Context(), tx, jsonqueries.Policies.Table, same)
		if err != nil || policy.ID != original[0].PolicyID {
			t.Fatal(policy, err)
		}
		updated, err := orm.Update(t.Context(), tx, q, f.Key(), 1, orm.NullableJSONSet(f.Backup, original[0].Settings))
		if err != nil || updated.Backup.IsNull() || updated.Settings != original[0].Settings {
			t.Fatal(updated, err)
		}
		cleared, err := orm.Update(t.Context(), tx, q, f.Key(), 1, orm.NullableJSONSetNull(f.Backup))
		if err != nil || !cleared.Backup.IsNull() || cleared.Settings != original[0].Settings {
			t.Fatal(cleared, err)
		}
		if _, err := orm.Update(t.Context(), tx, q, f.Key(), 1, orm.JSONSet(f.Settings, value.JSON[jsonqueries.Preferences]{})); err == nil {
			t.Fatal("zero JSON wrote")
		}
		kept, err := orm.RequireKey(t.Context(), tx, jsonqueries.Documents.Table, 1)
		if err != nil || kept.Settings != original[0].Settings {
			t.Fatal("failed write changed row", err)
		}
		price, err := decimal.Parse("9007199254740993.123456789")
		if err != nil {
			return err
		}
		prefs := document(t, jsonqueries.Preferences{Quota: value.Set(price), Labels: value.Set(map[string]string{"lang": "日本語"})})
		withQuota, err := orm.Update(t.Context(), tx, q, f.Key(), 1, orm.JSONSet(f.Settings, prefs))
		if err != nil || withQuota.Settings != prefs {
			t.Fatal(withQuota, err)
		}
		return nil
	})
}
