package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var source orm.InputTime[models.User, temporal.DateTime, temporal.DateTime]
var _ = orm.InputColumn[models.User, temporal.DateTime, temporal.DateTime](source)
