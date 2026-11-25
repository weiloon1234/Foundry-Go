// Package ctes exercises typed common table expressions.
package ctes

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin cte-aliases
type EligibleAlias struct{}
type OtherAlias struct{}

// doc-example:end cte-aliases

// SharedEligible builds the guide's shared CTE and the join that reads it twice.
func SharedEligible() (rows.CommonTable[models.User], rows.Scalar[rows.Inner[rows.Alias[EligibleAlias, models.User], rows.Alias[OtherAlias, models.User]], string]) {
	// doc-example:begin model-ctes
	u := models.Users
	eligible := rows.CTE("eligible_users", rows.ModelRecord(models.Users.Query().Where(orm.Gte(u.Age, 21)))).Materialized()
	first, second := rows.AsRecord[EligibleAlias](eligible, "first_user"), rows.AsRecord[OtherAlias](eligible, "second_user")
	a, b := rows.Scope(first), rows.Scope(second)
	joined := rows.InnerJoin(first, second, rows.On(expr.At(a, models.Users.ID), expr.At(b, models.Users.ID)))
	fields := rows.LeftScope(joined, rows.Scope(first))
	names := rows.OrderScalar(rows.SelectScalar(joined, expr.AsSelected(expr.Row(expr.At(fields, models.Users.Email)))), expr.Asc(expr.At(fields, models.Users.Email)))
	// doc-example:end model-ctes
	return eligible, names
}
