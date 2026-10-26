package invalid

import (
	"foundry.test/consumer/jsonqueries"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = jsonqueries.ProjectSnapshot(jsonqueries.QueryJsonDocuments()).SelectSettings(foundryquery.Value(jsonqueries.DocumentFields().Tags))
}
