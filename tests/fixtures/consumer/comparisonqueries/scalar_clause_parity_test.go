package comparisonqueries_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestAliasedScalarWindowSQLParity(t *testing.T) {
	q := models.Users.Query()
	a := rows.AsRecord[thresholdAlias](rows.CTE("adult_threshold", rows.ModelRecord(q.Where(orm.NumberGt(models.Users.Age, 20)))), "threshold")
	age := expr.At(rows.Scope(a), orm.NumberExpr(models.Users.Age))
	selected := rows.LimitScalar(rows.OffsetScalar(rows.OrderScalar(rows.SelectScalar(a, expr.AsSelected(expr.Row(age))), expr.Asc(age)), 1), 1)
	got, err := orm.Compile(expr.Where(q, expr.Equal(expr.NullableRow(expr.Value(orm.NumberExpr(models.Users.Age))), rows.ScalarRowQuery(rows.From(q), selected))))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/comparisonqueries/scalar_clause_parity_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
}
