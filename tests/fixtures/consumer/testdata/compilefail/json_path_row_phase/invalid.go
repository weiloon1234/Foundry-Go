package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = query.JSONRoot(jsonqueries.DocumentFields().Settings.Value())
}
