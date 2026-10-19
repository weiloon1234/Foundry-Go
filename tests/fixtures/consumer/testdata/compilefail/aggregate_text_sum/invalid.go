package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = foundryquery.Sum(models.UserFields().Email)
