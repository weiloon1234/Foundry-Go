package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

var _ = rows.UpdateFrom(models.WriteRecords.Query(), rows.From(models.Users.Query())).Select(rows.MapUpdate(orm.TextExpr(models.WriteRecords.Name), expr.Value(orm.TextExpr(models.WriteRecords.Name))))
