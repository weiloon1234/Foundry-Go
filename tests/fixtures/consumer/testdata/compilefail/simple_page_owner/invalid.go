package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong() {
	p, _ := models.Users.Query().SimplePaginate(nil, nil, orm.PageRequest{})
	var _ orm.SimplePage[models.Order] = p
}
