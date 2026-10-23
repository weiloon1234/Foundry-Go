package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func wrong() {
	item, _ := orm.Upsert(nil, nil, models.Users.Query(), orm.OnConflict[models.User]().DoNothing())
	var _ value.Optional[models.Order] = item
}
