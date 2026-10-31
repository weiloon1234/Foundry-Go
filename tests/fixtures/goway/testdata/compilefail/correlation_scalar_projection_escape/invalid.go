package correlation_scalar_projection_escape

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var parent = rows.From(models.Users.Query())
var child = rows.From(models.Orders.Query())
var link = rows.Correlate(parent, child)
var parentScope = rows.OuterScope(link, rows.ModelScope(models.Users.Query()))
var childScope = rows.InnerScope(link, rows.ModelScope(models.Orders.Query()))
var parentID = expr.At(parentScope, orm.KeyExpr(models.Users.ID))
var buyerID = expr.At(childScope, orm.ColumnExpr(models.Orders.BuyerID))
var selected = rows.SelectCorrelatedScalar(link.Where(expr.Equal(buyerID, parentID)), expr.AsSelected(expr.Row(buyerID)))

var _ = rows.FromRows(selected)
