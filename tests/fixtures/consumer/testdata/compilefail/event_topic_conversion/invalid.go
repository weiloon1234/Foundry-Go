package invalid

import (
	"foundry.test/consumer/eventqueries"
	"github.com/weiloon1234/Foundry-Go/events"
)

var wrong = events.Topic[int](eventqueries.Created)
