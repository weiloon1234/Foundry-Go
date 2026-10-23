package invalid

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
var ids = rows.SelectScalar(source, expr.Select(expr.At(rows.Scope(source), models.Users.ID)))
var nullable = rows.SelectScalar(source, expr.Select(expr.At(rows.Scope(source), models.Users.Nickname)))
var combined = rows.Union(record, record)
var tx = rows.TransactionCTE("claimed", rows.LockedRecord(models.Users.Query().ForUpdate()))
var invalid = rows.UnionValues(rows.Values(ids), rows.Values(rows.SelectScalar(rows.From(models.Orders.Query()), expr.Select(expr.Value(models.Orders.ID)))))
