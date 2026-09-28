package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = query.JSONContains(jsonqueries.DocumentFields().Backup, jsonqueries.DocumentFields().Settings)
}
