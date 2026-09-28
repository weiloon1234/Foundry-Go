package invalid

import (
	"foundry.test/consumer/linkqueries"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong model.ID[linkqueries.Group] = (linkqueries.Group{}).FoundryReference().Key()
