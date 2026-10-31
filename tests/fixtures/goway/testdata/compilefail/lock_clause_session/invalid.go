package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type a struct{}
type b struct{}

var source = rows.AsTransaction[a](rows.TransactionOf(rows.ModelRecord(models.Users.Query())), "users_alias")
var selected = rows.SelectTransactionValue(source, expr.AsSelected(expr.Row(expr.At(rows.TransactionScope(source), orm.KeyExpr(models.Users.ID)))))
var c = rows.CursorFor(rows.ModelRecord(models.Users.Query()))
var normal = rows.Read(rows.SelectValue(models.Users.Query(), orm.TextExpr(models.Users.Email)))
var input = rows.As[a](models.Users.Query(), "u")
var other = rows.As[b](models.Orders.Query(), "o")

func invalid(ctx context.Context, db *database.Session) {
	_, _ = rows.LockRows(normal, rows.UpdateLock(rows.Target(rows.ModelScope(models.Users.Query())))).All(ctx, db)
}
