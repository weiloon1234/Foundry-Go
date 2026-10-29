package expressionqueries_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func TestTypedConditionalSQL(t *testing.T) {
	u := models.Users
	display := expr.Coalesce(expr.Value(orm.NullableTextExpr(u.Nickname)), expr.Value(orm.TextExpr(u.Email)))
	choice := expr.When(expr.ModelPredicate(orm.NumberGt(u.Age, 20)), expr.Param(orm.TextExpr(u.Email), "older"))
	label := choice.When(expr.ModelPredicate(orm.NumberEq(u.Age, 20)), expr.Value(orm.TextExpr(u.Email))).Else(expr.Param(orm.TextExpr(u.Email), "unknown"))
	statement, err := rows.CompileRead(rows.ReadScalar(rows.OrderScalar(rows.SelectScalar(rows.From(expr.Where(models.Users.Query(), display.Ne(""))), label.Value()), label.Asc())))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CASE WHEN", "COALESCE(", " AS text)", "ORDER BY CASE"} {
		if !strings.Contains(statement.SQL(), want) {
			t.Fatal("missing computed SQL", want, statement.SQL())
		}
	}
	orders := models.Orders.Query()
	total := expr.Sum(orm.NumberExpr(models.Orders.TotalCents))
	computed := expr.CoalesceValue(total.Value(), total.Param(decimal.FromInt64(0)).Value())
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(orders), computed))); err != nil {
		t.Fatal(err)
	}
	count := expr.Count[models.Order]()
	category := expr.WhenValue(count.Gt(1), count.Param(2).Value()).Else(count.Param(1).Value())
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(orders), category))); err != nil {
		t.Fatal(err)
	}
}
