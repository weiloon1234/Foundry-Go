package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var source orm.UpdatedAt[models.User, temporal.DateTime]
var _ = orm.CreatedAt[models.User, temporal.DateTime](source)
