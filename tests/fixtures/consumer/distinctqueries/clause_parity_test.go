package distinctqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func assertClauseParity(t *testing.T, got orm.Statement, want query.Statement) {
	t.Helper()
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("clause SQL or bindings changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}

func TestScalarGroupingDistinctSQLParity(t *testing.T) {
	q, oldQ := models.Users.Query(), models.QueryUsers()
	count, oldCount := expr.Count[models.User](), query.Count[models.User]()
	selected := rows.SelectScalar(rows.From(q.Where(models.Users.Age.Gt(18))), count.Value())
	grouped := rows.HavingScalar(rows.GroupByScalar(selected, expr.Group(expr.Value(models.Users.ID))), count.Gt(0))
	oldGrouped := query.SelectValue(oldQ.Where(models.UserFields().Age.Gt(18)), oldCount.Value()).GroupBy(models.UserFields().ID.Group()).Having(oldCount.Gt(0))
	for _, tc := range []struct {
		name    string
		current rows.Scalar[models.User, int64]
		legacy  query.ValueQuery[models.User, int64]
	}{
		{"distinct", rows.OrderScalar(rows.DistinctScalar(grouped), count.Desc()), oldGrouped.Distinct().OrderBy(oldCount.Desc())},
		{"selected_key", rows.OrderScalar(rows.DistinctOnScalarValues(grouped, count.Value().Key()), count.Desc()), oldGrouped.DistinctOnValues(oldCount.Value().Key()).OrderBy(oldCount.Desc())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orm.Compile(expr.Where(q, expr.Equal(rows.ScalarRowQuery(rows.From(q), tc.current), expr.NullableRow(count.Param(1)))))
			if err != nil {
				t.Fatal(err)
			}
			want, err := oldQ.Where(query.Equal(query.ScalarRowQuery(oldQ, tc.legacy), query.NullableRow(oldCount.Param(1)))).Compile()
			if err != nil {
				t.Fatal(err)
			}
			assertClauseParity(t, got, want)
		})
	}
}

func TestScalarDistinctOnUnselectedKeySQLParity(t *testing.T) {
	q, oldQ := models.Orders.Query(), models.QueryOrders()
	o, oldO := models.Orders, models.OrderFields()
	selected := rows.OrderScalar(rows.DistinctOnScalar(rows.SelectScalar(rows.From(q.Where(o.TotalCents.Gt(0))), expr.Select(expr.Value(o.TotalCents))), expr.Group(expr.Value(o.BuyerID))), expr.Value(o.BuyerID).Asc(), expr.Value(o.TotalCents).Desc())
	got, err := orm.Compile(expr.Where(q, rows.InQuery(expr.Value(o.TotalCents), selected)))
	if err != nil {
		t.Fatal(err)
	}
	oldSelected := query.SelectValue(oldQ.Where(oldO.TotalCents.Gt(0)), query.Value(oldO.TotalCents)).DistinctOn(oldO.BuyerID.Group()).OrderBy(oldO.BuyerID.Asc(), oldO.TotalCents.Desc())
	want, err := oldQ.Where(oldO.TotalCents.InQuery(oldSelected)).Compile()
	if err != nil {
		t.Fatal(err)
	}
	assertClauseParity(t, got, want)
}

func TestRecordAndSetDistinctSQLParity(t *testing.T) {
	q, oldQ := models.Users.Query(), models.QueryUsers()
	a, oldA := rows.As[personAlias](q, "person"), query.As[personAlias](oldQ, "person")
	record := rows.DistinctRecord(rows.SelectRecord(a, rows.Scope(a)))
	oldRecord := query.SelectRecord(oldA, oldA.Scope()).Distinct()
	compare := func(t *testing.T, actual rows.RecordSource[models.User], legacy query.RecordQuerySource[models.User]) {
		t.Helper()
		alias := rows.AsRecord[resultAlias](rows.CTE("unique_people", actual), "chosen")
		oldAlias := query.As[resultAlias](query.CTE("unique_people", legacy), "chosen")
		got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), alias)))
		if err != nil {
			t.Fatal(err)
		}
		want, err := oldQ.Where(query.ExistsQuery(oldQ, oldAlias)).Compile()
		if err != nil {
			t.Fatal(err)
		}
		assertClauseParity(t, got, want)
	}
	t.Run("record", func(t *testing.T) { compare(t, record.Offset(1).Limit(2), oldRecord.Offset(1).Limit(2)) })
	t.Run("set", func(t *testing.T) {
		compare(t, rows.DistinctSet(rows.UnionAll(record, rows.ModelRecord(q))).Offset(1).Limit(2), oldRecord.UnionAll(oldQ).Distinct().Offset(1).Limit(2))
	})
}

func TestValueSetDistinctSQLParity(t *testing.T) {
	q, oldQ := models.Users.Query(), models.QueryUsers()
	source := rows.DistinctScalar(rows.SelectScalar(rows.From(q), expr.Select(expr.Value(models.Users.Nickname))))
	combined := rows.DistinctValues(rows.UnionAllValues(rows.Values(source), rows.Values(source)))
	combined = combined.OrderBy(combined.Value().Asc()).Offset(1).Limit(2)
	got, err := orm.Compile(expr.Where(q, rows.ExistsQuery(rows.From(q), combined)))
	if err != nil {
		t.Fatal(err)
	}
	oldSource := query.SelectValue(oldQ, query.Value(models.UserFields().Nickname)).Distinct()
	oldCombined := oldSource.UnionAll(oldSource)
	want, err := oldQ.Where(query.ExistsQuery(oldQ, oldCombined.Distinct().OrderBy(oldCombined.Value().Asc()).Offset(1).Limit(2))).Compile()
	if err != nil {
		t.Fatal(err)
	}
	assertClauseParity(t, got, want)
}
