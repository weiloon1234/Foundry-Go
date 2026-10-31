package jsonqueries_test

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGeneratedJSONPaths(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.Documents.Query(), jsonqueries.Documents
		prefs := expr.JSONRoot(expr.Value(orm.JSONExpr(f.Settings)))
		price, err := decimal.Parse("9007199254740993.123456789")
		if err != nil {
			return err
		}
		profile := jsonqueries.Profile{Name: "Ada", Age: 42, Scores: []int64{1, 7}, Attributes: map[int]string{2: "two"}, Next: &jsonqueries.Profile{Name: "next"}, Quoted: "quoted\"value", Odd: "safe'_%"}
		updated := document(t, jsonqueries.Preferences{Theme: value.Set("dark"), Labels: value.Set(map[string]string{"lang": "en"}), Quota: value.Set(price), Profile: value.Set(profile)})
		if _, err := orm.Update(t.Context(), tx, q, f.Key(), 1, orm.JSONSet(f.Settings, updated)); err != nil {
			return err
		}
		p := expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.ProfileField))
		for _, test := range []struct {
			predicate expr.Predicate[jsonqueries.Document]
			want      int64
		}{
			{expr.NullableTextLike(expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.Theme)), codec.String[string]()), "%ark%"), 2},
			{expr.NullableTextContains(expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.Theme)), codec.String[string]()), "ar"), 2},
			{expr.IsNull(expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.Theme)).JSON()), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONMapKey(expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.LabelsField)), "lang"), codec.String[string]()), "en"), 1},
			{expr.NullableEq(expr.OrderNullableRow(expr.JSONScalar(expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.QuotaField)), codec.Decimal())), price), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONIndex(expr.JSONRoot(expr.Value(orm.JSONExpr(f.Tags))), -1), codec.String[string]()), "go"), 2},
			{expr.IsNull(expr.JSONIndex(expr.JSONRoot(expr.Value(orm.JSONExpr(f.Tags))), 100).JSON()), 3},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONProperty(p, jsonqueries.NameField), codec.String[string]()), "Ada"), 1},
			{expr.NullableEq(expr.OrderNullableRow(expr.JSONScalar(expr.JSONProperty(p, jsonqueries.AgeField), codec.Signed[int64]())), 42), 1},
			{expr.Eq(expr.JSONTypeNullable(expr.JSONProperty(p, jsonqueries.NicknameField).JSON()), value.Of(database.JSONNull)), 1},
			{expr.IsNotNull(expr.JSONProperty(p, jsonqueries.NicknameField).JSON()), 1},
			{expr.IsNull(expr.JSONProperty(p, jsonqueries.NicknameField).JSON()), 2},
			{expr.IsNull(expr.JSONTextScalar(expr.JSONPointer(expr.JSONProperty(p, jsonqueries.NicknameField)), codec.String[string]())), 3},
			{expr.NullableGt(expr.OrderNullableRow(expr.JSONScalar(expr.JSONIndex(expr.JSONProperty(p, jsonqueries.ScoresField), -1), codec.Signed[int64]())), 6), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONMapKey(expr.JSONProperty(p, jsonqueries.AttributesField), 2), codec.String[string]()), "two"), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONProperty(expr.JSONPointer(expr.JSONProperty(p, jsonqueries.NextField)), jsonqueries.NameField), codec.String[string]()), "next"), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONProperty(p, jsonqueries.QuotedField), codec.String[string]()), "quoted\"value"), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONProperty(p, jsonqueries.OddField), codec.String[string]()), "safe'_%"), 1},
			{expr.NullableEq(expr.JSONTextScalar(expr.JSONOptional(expr.JSONProperty(expr.JSONNullableRoot(expr.Value(orm.NullableJSONExpr(f.Backup))), jsonqueries.Theme)), codec.String[string]()), "dark"), 1},
		} {
			got, err := orm.Count(t.Context(), tx, expr.Where(q, test.predicate))
			if err != nil || got != test.want {
				t.Fatal("JSON path count", got, test.want, err)
			}
		}
		snapshots, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.OrderBy(orm.KeyAsc(f.Key()))), expr.AsSelected(expr.JSONOptional(expr.JSONProperty(prefs, jsonqueries.ProfileField)).JSON()))).All(t.Context(), tx)
		if err != nil || len(snapshots) != 3 || !snapshots[1].IsNull() || !snapshots[2].IsNull() {
			t.Fatal(snapshots, err)
		}
		snapshot, ok := snapshots[0].Get()
		decoded, err := snapshot.Decode()
		if !ok || err != nil || decoded.Name != "Ada" || decoded.Age != 42 {
			t.Fatal(decoded, err)
		}
		nulls, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.OrderBy(orm.KeyAsc(f.Key()))), expr.AsSelected(expr.JSONProperty(p, jsonqueries.NicknameField).JSON()))).All(t.Context(), tx)
		if err != nil || len(nulls) != 3 || !nulls[1].IsNull() {
			t.Fatal(nulls, err)
		}
		first, present := nulls[0].Get()
		if !present || !first.IsJSONNull() {
			t.Fatal("JSON null lost", nulls)
		}
		ages, err := rowset.ReadScalar(rowset.SelectScalar(rowset.From(q.Where(orm.KeyEq(f.Key(), 1))), expr.AsSelected(expr.JSONProperty(p, jsonqueries.AgeField).JSON()))).All(t.Context(), tx)
		if err != nil || len(ages) != 1 {
			t.Fatal(ages, err)
		}
		age, ok := ages[0].Get()
		actual, err := age.Decode()
		if !ok || err != nil || actual != 42 {
			t.Fatal(actual, err)
		}
		return nil
	})
}
