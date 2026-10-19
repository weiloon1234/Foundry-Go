package invalid

import (
	"foundry.test/consumer/jsonqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() { _ = foundryquery.Sum(jsonqueries.DocumentFields().Settings) }
