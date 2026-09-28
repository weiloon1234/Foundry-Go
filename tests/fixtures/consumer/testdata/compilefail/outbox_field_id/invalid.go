package invalid

import (
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

var invalid = eventqueries.EventLinkDraft{}.SetMessageID(outbox.ID[string]{})
