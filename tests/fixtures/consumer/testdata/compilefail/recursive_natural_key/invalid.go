package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/recursivequeries"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type parentAlias struct{}

func invalid(self rows.RecursiveSelf[recursivequeries.Node]) {
	parent := rows.AsRecord[parentAlias](self, "parent")
	_ = expr.At(rows.Scope(parent), orm.TextExpr(recursivequeries.Nodes.Code)).Eq(models.User{}.ID)
}
