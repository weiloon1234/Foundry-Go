package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryUsers().DistinctOn(foundryquery.Value(models.UserFields().Age).Key())
