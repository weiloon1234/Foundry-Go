package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = query.JSONRoot(query.Value(jsonqueries.DocumentFields().Settings))
}
