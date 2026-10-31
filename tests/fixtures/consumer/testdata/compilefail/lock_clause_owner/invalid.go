package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var invalid = rows.LockRows(rows.ReadScalar(rows.SelectScalar(rows.From(models.Users.Query()), expr.AsSelected(expr.Row(expr.Value(orm.TextExpr(models.Users.Email)))))), rows.KeyShareLock(rows.Target(rows.ModelScope(models.Countries.Query()))))
