package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() {
	_ = jsonqueries.QueryJsonPolicies().Where(jsonqueries.DocumentFields().Settings.Eq(value.JSON[jsonqueries.Preferences]{}))
}
