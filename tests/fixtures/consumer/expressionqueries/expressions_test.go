package expressionqueries_test

import (
	"strings"
	"testing"

	"foundry.test/consumer/expressionqueries"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestTypedConditionalSQL(t *testing.T) {
	statement, err := rows.CompileRead(rows.ReadScalar(expressionqueries.LabelsByAge()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CASE WHEN", "COALESCE(", " AS text)", "ORDER BY CASE"} {
		if !strings.Contains(statement.SQL(), want) {
			t.Fatal("missing computed SQL", want, statement.SQL())
		}
	}
	orders := models.Orders.Query()
	computed := expressionqueries.TotalOrZero()
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(orders), computed))); err != nil {
		t.Fatal(err)
	}
	count := expr.Count[models.Order]()
	category := expr.WhenValue(expr.GtValue(count, 1), expr.AsSelected(expr.ParamOf(count, 2))).Else(expr.AsSelected(expr.ParamOf(count, 1)))
	if _, err := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(orders), category))); err != nil {
		t.Fatal(err)
	}
}
