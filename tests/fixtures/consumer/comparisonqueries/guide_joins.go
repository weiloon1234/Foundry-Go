// Package comparisonqueries exercises typed value comparisons and join conditions.
package comparisonqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin comparison-aliases
type YoungerAlias struct{}
type OlderAlias struct{}

// doc-example:end comparison-aliases

// AdjacentAges builds the guide's self-join over users whose ages differ by one.
func AdjacentAges() (orm.Query[models.User], rows.Aliased[YoungerAlias, models.User], rows.Aliased[OlderAlias, models.User], expr.Scope[rows.Alias[YoungerAlias, models.User], models.User], expr.Scope[rows.Alias[OlderAlias, models.User], models.User], rows.JoinOn[rows.Alias[YoungerAlias, models.User], rows.Alias[OlderAlias, models.User]]) {
	// doc-example:begin value-comparisons-and-joins
	q := models.Users.Query()
	younger, older := rows.As[YoungerAlias](q, "younger"), rows.As[OlderAlias](q, "older")
	y, o := rows.Scope(younger), rows.Scope(older)
	adjacent := rows.OnEqual(expr.Add(expr.At(y, models.Users.Age), expr.ParamOf(expr.AsSelected(expr.Row(expr.At(y, models.Users.Age))), 1)), expr.At(o, models.Users.Age))
	// doc-example:end value-comparisons-and-joins
	return q, younger, older, y, o, adjacent
}
