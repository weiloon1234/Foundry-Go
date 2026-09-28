package scalarqueries_test

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/scalarqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type computedLines struct{}
type matchingSample struct{}
type previousSample struct{}

func TestPostgresScalarProjectionAndDerivedAggregation(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.QueryCalculationSamples()
		f := scalarqueries.SampleFields()
		selected := scalarqueries.SelectCalculation(q, scalarqueries.CalculationSelection[scalarqueries.Sample]{
			ID: f.ID.Value(), Caption: query.Lower(query.Trim(f.Name)).Value(),
			LineTotal:     query.Multiply(query.DecimalOf(f.Quantity), f.Amount).Value(),
			AmountWithTax: query.AddNullable(query.NullableRow(f.Amount), f.Tax).Value(),
		})
		rows, err := selected.OrderBy(f.ID.Asc()).All(t.Context(), tx)
		if err != nil || len(rows) != 3 {
			t.Fatal(rows, err)
		}
		if rows[0].Caption != "élan_20%" {
			t.Fatal("text projection", rows[0])
		}
		if _, present := rows[1].AmountWithTax.Get(); present {
			t.Fatal("nullable projection lost NULL")
		}
		derived := query.As[computedLines](query.CTE("computed_lines", selected), "lines")
		line := scalarqueries.CalculationFieldsAt(derived.Scope())
		want, _ := decimal.Parse("18014398509481997.45")
		total, err := query.SelectValue(derived, line.LineTotal.Sum().Value()).RequireFirst(t.Context(), tx)
		got, present := total.Get()
		if err != nil || !present || got != want {
			t.Fatal("exact derived arithmetic aggregation", got, err)
		}
		return nil
	})
}

func TestPostgresScalarCTEDiscoveryAndCorrelations(t *testing.T) {
	runCalculations(t, func(tx *database.Tx) error {
		q := scalarqueries.QueryCalculationSamples()
		f := scalarqueries.SampleFields()
		matching := query.As[matchingSample](query.CTE("positive_quantity", q.Where(query.Abs(f.Quantity).Gt(2))), "matching")
		m := scalarqueries.SampleFieldsAt(matching.Scope())
		matched := query.When(f.ID.InQuery(query.SelectValue(matching, m.ID.Value())), f.ID.Param(10)).Else(f.ID.Param(0))
		scalarValues(t, tx, query.Add(matched, f.ID).Value(), []int{1, 12, 13})
		previous := query.As[previousSample](q, "previous")
		link := query.Correlate(q, previous)
		parent := scalarqueries.SampleFieldsAt(query.OuterScope(link, q.Scope()))
		child := scalarqueries.SampleFieldsAt(query.InnerScope(link, previous.Scope()))
		caption := query.Concat(query.Lower(parent.Name), query.Lower(child.Name))
		correlated := query.SelectCorrelatedValue(link, caption.Value()).Where(child.ID.LtColumn(parent.ID)).OrderBy(child.ID.Desc()).Limit(1)
		scalarValues(t, tx, query.CorrelatedScalarQuery(correlated), []value.Nullable[string]{value.Null[string](), value.Of("beta élan_20% "), value.Of("beta")})
		return nil
	})
}

func TestPostgresComputedManyToManyOrdering(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		if _, err := tx.Exec(t.Context(), `CREATE TABLE friendships (id uuid PRIMARY KEY,from_id uuid NOT NULL,to_id uuid NOT NULL,note text NOT NULL)`); err != nil {
			return err
		}
		for i, note := range []string{"B", "a"} {
			if _, err := models.QueryFriendships().Create(t.Context(), tx, models.FriendshipDraft{}.SetFromID(users[0].ID).SetToID(users[i+1].ID).SetNote(note)); err != nil {
				return err
			}
		}
		u := models.UserFields()
		p := models.FriendshipFields()
		for _, rel := range []query.ThroughRelation[models.User, models.User, models.Friendship]{
			models.UserRelations().Friends.OrderBy(query.Negate(u.Age).Asc()),
			models.UserRelations().Friends.OrderByPivot(query.Lower(p.Note).Asc()),
		} {
			rows, err := models.QueryUsers().Where(u.ID.Eq(users[0].ID)).With(rel).All(t.Context(), tx)
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			links, loaded := rows[0].Friends.Get()
			if !loaded || len(links) != 2 || links[0].Model.ID != users[2].ID {
				t.Fatal("computed target/pivot qualification or order", links)
			}
		}
		return nil
	})
}
