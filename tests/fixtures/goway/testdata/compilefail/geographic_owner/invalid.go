package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type other struct{}

func invalid() {
	_ = expr.SphericalDistance(expr.Value(orm.NumberExpr(scalars.Records.Latitude)), expr.At(rows.Scope(rows.As[other](scalars.Records.Query(), "s")), orm.NumberExpr(scalars.Records.Latitude)), expr.GeographicPoint{})
}
