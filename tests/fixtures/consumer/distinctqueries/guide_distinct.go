// Package distinctqueries exercises typed DISTINCT and DISTINCT ON reads.
package distinctqueries

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

// doc-example:begin distinct-aliases
type PersonAlias struct{}
type PurchaseAlias struct{}

// doc-example:end distinct-aliases

type buyerJoin = rows.Inner[rows.Alias[PersonAlias, models.User], rows.Alias[PurchaseAlias, models.Order]]

// BuyerRecords builds the guide's ordered buyer records and their distinct selection.
func BuyerRecords() (rows.InnerJoined[rows.Alias[PersonAlias, models.User], rows.Alias[PurchaseAlias, models.Order]], expr.Scope[buyerJoin, models.User], rows.RecordRead[buyerJoin, models.User], rows.RecordRead[buyerJoin, models.User]) {
	// doc-example:begin model-distinct
	people := rows.As[PersonAlias](models.Users.Query(), "person")
	orders := rows.As[PurchaseAlias](models.Orders.Query(), "purchase")
	p, o := rows.Scope(people), rows.Scope(orders)
	joined := rows.InnerJoin(people, orders, rows.On(expr.At(p, models.Users.ID), expr.At(o, models.Orders.BuyerID)))
	scope := rows.LeftScope(joined, rows.Scope(people))
	base := rows.SelectRecord(joined, scope).OrderBy(expr.Asc(expr.At(scope, models.Users.Email)))
	unique := rows.DistinctRecord(base)
	// doc-example:end model-distinct
	return joined, scope, base, unique
}
