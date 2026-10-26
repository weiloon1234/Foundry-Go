package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}

var source = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "users_alias")
var selected = rows.SelectTransactionValue(source, expr.Select(expr.At(rows.TransactionScope(source), models.Users.ID)))
var c = rows.CursorFor(rows.ModelRecord(models.Users.Query()))
var normal = rows.Read(rows.SelectValue(models.Users.Query(), models.Users.Email))
var input = rows.As[a](models.Users.Query(), "u")
var other = rows.As[b](models.Orders.Query(), "o")
var _, _ = rows.ValueCursorFor(rows.Values(rows.SelectScalar(rows.From(models.Users.Query()), expr.Select(expr.Value(models.Users.Nickname))))).Paginate(nil, nil, orm.CursorRequest[string]{Size: 1})
