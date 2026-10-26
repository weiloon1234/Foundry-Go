package correlation_input_selection

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
var parentID = expr.At(parentScope, models.Users.ID)
var buyerID = expr.At(childScope, models.Orders.BuyerID)
var selected = rows.SelectCorrelatedScalar(link.Where(expr.Equal(buyerID, parentID)), expr.Row(buyerID).Value())

var _ = rows.SelectCorrelatedScalar(link, expr.Row(expr.Value(models.Orders.ID)).Value())
