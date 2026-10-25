package comparisonqueries_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestAliasedScalarWindowSQLParity(t *testing.T) {
	q := models.Users.Query()
	a := rows.AsRecord[thresholdAlias](rows.CTE("adult_threshold", rows.ModelRecord(q.Where(models.Users.Age.Gt(20)))), "threshold")
	age := expr.At(rows.Scope(a), models.Users.Age)
	selected := rows.LimitScalar(rows.OffsetScalar(rows.OrderScalar(rows.SelectScalar(a, expr.Select(age)), age.Asc()), 1), 1)
	got, err := orm.Compile(expr.Where(q, expr.Equal(expr.NullableRow(expr.Value(models.Users.Age)), rows.ScalarRowQuery(rows.From(q), selected))))
	if err != nil {
		t.Fatal(err)
	}
	oldQ := models.QueryUsers()
	oldA := query.As[thresholdAlias](query.CTE("adult_threshold", oldQ.Where(models.UserFields().Age.Gt(20))), "threshold")
	oldAge := models.UserFieldsAt(oldA.Scope()).Age
	oldSelected := query.SelectValue(oldA, query.Value(oldAge)).OrderBy(oldAge.Asc()).Offset(1).Limit(1)
	want, err := oldQ.Where(query.Equal(query.NullableRow(models.UserFields().Age), query.ScalarRowQuery(oldQ, oldSelected))).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("scalar boundary changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}
