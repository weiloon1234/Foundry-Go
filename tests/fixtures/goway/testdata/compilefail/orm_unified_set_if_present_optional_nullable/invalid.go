package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

var opt value.Optional[value.Nullable[string]]
var _ = orm.SetIfPresent(nil, models.Users.Nickname, opt)
