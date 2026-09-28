package invalid

import (
	"foundry.test/consumer/eventqueries"
	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/model"
)

var wrong = eventqueries.RecordCreated{ID: model.ID[observerqueries.Record]{}}
