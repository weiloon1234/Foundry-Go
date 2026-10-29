package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong() {
	p, _ := orm.SimplePaginate(nil, nil, models.Users.Query(), orm.PageRequest{})
	var _ orm.SimplePage[models.Order] = p
}
