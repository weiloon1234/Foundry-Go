package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func wrong() {
	p, _ := rows.SimplePaginate(nil, nil, rows.SelectValue(models.Users.Query(), models.Users.Nickname), orm.PageRequest{})
	var _ orm.SimplePage[string] = p
}
