package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var source orm.CreatedAt[models.User, temporal.DateTime]
var _ = orm.Time[models.User, temporal.DateTime](source)
