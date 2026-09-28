package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.ManyToMany(models.UserFields().ID, models.MembershipFields().UserID, models.MembershipFields().GroupCode, models.UserFields().ID)
