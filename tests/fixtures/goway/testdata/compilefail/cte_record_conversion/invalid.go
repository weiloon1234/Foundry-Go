package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type tag struct{}
type other struct{}

var record = rows.ModelRecord(models.Users.Query())
var cte = rows.CTE("users_copy", record)
var source = rows.AsRecord[tag](cte, "u")
var ids = rows.SelectScalar(source, expr.Row(expr.At(rows.Scope(source), orm.KeyExpr(models.Users.ID))).Value())
var nullable = rows.SelectScalar(source, expr.Row(expr.At(rows.Scope(source), orm.NullableTextExpr(models.Users.Nickname))).Value())
var combined = rows.Union(record, record)
var tx = rows.TransactionCTE("claimed", rows.LockedRecord(orm.Lock(models.Users.Query())))
var invalid = rows.CommonTable[models.Order](cte)
