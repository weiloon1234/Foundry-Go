package invalid

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong() {
	p, _ := models.Users.Query().SimplePaginate(nil, nil, orm.Page(1, 2))
	var _ orm.SimplePage[models.Order] = p
}
