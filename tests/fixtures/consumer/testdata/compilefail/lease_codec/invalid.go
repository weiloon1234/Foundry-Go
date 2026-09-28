package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong = lease.Define[model.ID[mutatorqueries.Member]]("members", keyspace.TextKeys[model.ID[models.User]]())
