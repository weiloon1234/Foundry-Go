package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

type other struct{}

func invalid() {
	_ = expr.SphericalDistance(expr.Value(scalars.Records.Latitude), expr.At(rows.Scope(rows.As[other](scalars.Records.Query(), "s")), scalars.Records.Latitude), expr.GeographicPoint{})
}
