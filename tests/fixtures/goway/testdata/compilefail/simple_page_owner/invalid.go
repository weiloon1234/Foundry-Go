package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong() {
	p, _ := orm.SimplePaginate(nil, nil, models.Users.Query(), orm.Page(1, 2))
	var _ orm.SimplePage[models.Order] = p
}
