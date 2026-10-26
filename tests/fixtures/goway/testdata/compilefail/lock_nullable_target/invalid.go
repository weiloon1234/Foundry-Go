package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}

var source = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "users_alias")
var selected = rows.SelectTransactionValue(source, expr.Row(expr.At(rows.TransactionScope(source), models.Users.ID)).Value())
var c = rows.CursorFor(rows.ModelRecord(models.Users.Query()))
var normal = rows.Read(rows.SelectValue(models.Users.Query(), models.Users.Email))
var input = rows.As[a](models.Users.Query(), "u")
var other = rows.As[b](models.Orders.Query(), "o")
var joined = rows.LeftJoin(input, other, rows.On(expr.At(rows.Scope(input), models.Users.ID), expr.At(rows.Scope(other), models.Orders.BuyerID)))
var invalid = rows.Target(rows.NullableRightScope(joined, rows.Scope(other)))
