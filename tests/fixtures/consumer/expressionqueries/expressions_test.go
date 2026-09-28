package expressionqueries_test

import (
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func TestTypedConditionalSQL(t *testing.T) {
	u := models.UserFields()
	display := query.Coalesce(u.Nickname, u.Email)
	choice := query.When(u.Age.Gt(20), u.Email.Param("older"))
	label := choice.When(u.Age.Eq(20), u.Email).Else(u.Email.Param("unknown"))
	statement, err := query.SelectValue(models.QueryUsers().Where(display.Ne("")), label.Value()).OrderBy(label.Asc()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CASE WHEN", "COALESCE(", " AS text)", "ORDER BY CASE"} {
		if !strings.Contains(statement.SQL(), want) {
			t.Fatal("missing computed SQL", want, statement.SQL())
		}
	}
	orders := models.QueryOrders()
	total := models.OrderFields().TotalCents.Sum()
	computed := query.CoalesceValue(total.Value(), total.Param(decimal.FromInt64(0)).Value())
	if _, err := query.SelectValue(orders, computed).Compile(); err != nil {
		t.Fatal(err)
	}
	count := query.Count[models.Order]()
	category := query.WhenValue(count.Gt(1), count.Param(2).Value()).Else(count.Param(1).Value())
	if _, err := query.SelectValue(orders, category).Compile(); err != nil {
		t.Fatal(err)
	}
}
