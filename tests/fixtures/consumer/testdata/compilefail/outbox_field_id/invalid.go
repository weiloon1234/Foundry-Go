package invalid

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

var invalid = orm.Set(eventqueries.Links.MessageID, outbox.ID[string]{})
