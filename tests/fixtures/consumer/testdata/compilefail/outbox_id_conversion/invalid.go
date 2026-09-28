package invalid

import (
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

var invalid = outbox.ID[eventqueries.RecordCreated](outbox.ID[string]{})
