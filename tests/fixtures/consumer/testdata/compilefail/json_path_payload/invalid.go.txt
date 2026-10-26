package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() {
	var _ query.RowExpression[jsonqueries.Document, value.Nullable[value.JSON[int]]] = jsonqueries.DocumentFields().Settings.Properties().Theme.JSON()
}
