package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/relation"
)

var invalid = relation.Link[models.Group, models.Membership]{Model: models.Group{}, Pivot: models.Friendship{}}
