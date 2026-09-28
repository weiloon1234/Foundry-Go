package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = models.UserRelationSet{Introducer: query.HasOne(models.UserFields().ID, models.OrderFields().BuyerID)}
