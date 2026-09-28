package invalid

import (
	"foundry.test/consumer/linkqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong model.Reference[linkqueries.Member, model.ID[mutatorqueries.Member]] = (mutatorqueries.Member{}).FoundryReference()
