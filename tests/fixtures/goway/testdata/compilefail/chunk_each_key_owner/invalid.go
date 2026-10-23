package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong() {
	_ = orm.EachByKey(nil, nil, models.Users.Query(), models.Users.ID, 2, func(models.Order) error { return nil })
}
