package invalid

import (
	"foundry.test/consumer/jsonqueries"
	"github.com/weiloon1234/Foundry-Go/value"
)

func invalid() { _ = jsonqueries.DocumentFields().Settings.Contains(value.JSON[[]string]{}) }
