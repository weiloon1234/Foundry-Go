package jsonqueries_test

import (
	"testing"

	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresGeneratedJSONPaths(t *testing.T) {
	runJSON(t, func(tx *database.Tx, original []jsonqueries.Document) error {
		q, f := jsonqueries.QueryJsonDocuments(), jsonqueries.DocumentFields()
		prefs := f.Settings.Properties()
		price, err := decimal.Parse("9007199254740993.123456789")
		if err != nil {
			return err
		}
		profile := jsonqueries.Profile{Name: "Ada", Age: 42, Scores: []int64{1, 7}, Attributes: map[int]string{2: "two"}, Next: &jsonqueries.Profile{Name: "next"}, Quoted: "quoted\"value", Odd: "safe'_%"}
		updated := document(t, jsonqueries.Preferences{Theme: value.Set("dark"), Labels: value.Set(map[string]string{"lang": "en"}), Quota: value.Set(price), Profile: value.Set(profile)})
		if _, err := q.Update(t.Context(), tx, 1, jsonqueries.DocumentDraft{}.SetSettings(updated)); err != nil {
			return err
		}
		p := prefs.Profile.Properties()
		for _, test := range []struct {
			predicate query.Predicate[jsonqueries.Document]
			want      int64
		}{
			{prefs.Theme.Scalar().Like("%ark%"), 2},
			{prefs.Theme.Scalar().Contains("ar"), 2},
			{prefs.Theme.IsMissing(), 1},
			{prefs.Labels.At("lang").Scalar().Eq("en"), 1},
			{prefs.Quota.Scalar().Eq(price), 1},
			{f.Tags.At(-1).Scalar().Eq("go"), 2},
			{f.Tags.At(100).IsMissing(), 3},
			{p.Name.Scalar().Eq("Ada"), 1},
			{p.Age.Scalar().Eq(42), 1},
			{p.Nickname.IsJSONNull(), 1},
			{p.Nickname.Exists(), 1},
			{p.Nickname.IsMissing(), 2},
			{p.Nickname.Scalar().IsNull(), 3},
			{p.Scores.At(-1).Scalar().Gt(6), 1},
			{p.Attributes.At(2).Scalar().Eq("two"), 1},
			{p.Next.Properties().Name.Scalar().Eq("next"), 1},
			{p.Quoted.Scalar().Eq("quoted\"value"), 1},
			{p.Odd.Scalar().Eq("safe'_%"), 1},
			{f.Backup.Properties().Theme.Scalar().Eq("dark"), 1},
		} {
			got, err := q.Where(test.predicate).Count(t.Context(), tx)
			if err != nil || got != test.want {
				t.Fatal("JSON path count", got, test.want, err)
			}
		}
		snapshots, err := query.SelectValue(q.OrderBy(f.ID.Asc()), prefs.Profile.JSON().Value()).All(t.Context(), tx)
		if err != nil || len(snapshots) != 3 || !snapshots[1].IsNull() || !snapshots[2].IsNull() {
			t.Fatal(snapshots, err)
		}
		snapshot, ok := snapshots[0].Get()
		decoded, err := snapshot.Decode()
		if !ok || err != nil || decoded.Name != "Ada" || decoded.Age != 42 {
			t.Fatal(decoded, err)
		}
		nulls, err := query.SelectValue(q.OrderBy(f.ID.Asc()), p.Nickname.JSON().Value()).All(t.Context(), tx)
		if err != nil || len(nulls) != 3 || !nulls[1].IsNull() {
			t.Fatal(nulls, err)
		}
		first, present := nulls[0].Get()
		if !present || !first.IsJSONNull() {
			t.Fatal("JSON null lost", nulls)
		}
		ages, err := query.SelectValue(q.Where(f.ID.Eq(1)), p.Age.JSON().Value()).All(t.Context(), tx)
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
