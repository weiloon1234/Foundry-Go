package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var source orm.InputInterval[models.User, string]
var _ = orm.InputColumn[models.User, string, temporal.Interval](source)
