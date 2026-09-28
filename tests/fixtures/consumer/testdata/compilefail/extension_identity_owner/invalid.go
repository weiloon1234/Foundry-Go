package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/profiles"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.IdentityOf(profiles.QueryProfiles().Query, models.UserFields().ID)
