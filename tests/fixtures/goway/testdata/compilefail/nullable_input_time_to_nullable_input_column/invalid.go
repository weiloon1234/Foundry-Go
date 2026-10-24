package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var source orm.NullableInputTime[models.User, temporal.DateTime, temporal.DateTime]
var _ = orm.NullableInputColumn[models.User, temporal.DateTime, temporal.DateTime](source)
