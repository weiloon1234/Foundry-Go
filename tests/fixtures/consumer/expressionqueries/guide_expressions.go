// Package expressionqueries exercises typed conditional expressions.
package expressionqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

// doc-example:begin conditional-expressions
func LabelsByAge() rows.Scalar[models.User, string] {
	u := models.Users
	display := expr.Coalesce(expr.Value(u.Nickname), expr.Value(u.Email))
	choice := expr.When(expr.ModelPredicate(orm.Gt(u.Age, 20)), expr.Param(u.Email, "older"))
	label := choice.When(expr.ModelPredicate(orm.Eq(u.Age, 20)), expr.Value(u.Email)).Else(expr.Param(u.Email, "unknown"))
	return rows.OrderScalar(rows.SelectScalar(rows.From(expr.Where(models.Users.Query(), expr.Ne(display, ""))), expr.AsSelected(label)), expr.Asc(label))
}

// doc-example:end conditional-expressions

// doc-example:begin conditional-expressions-2
func TotalOrZero() expr.Selected[models.Order, decimal.Decimal] {
	total := expr.Sum(models.Orders.TotalCents)
	return expr.CoalesceValue(expr.AsSelected(total), expr.AsSelected(expr.ParamOf(total, decimal.FromInt64(0))))
}

// doc-example:end conditional-expressions-2
