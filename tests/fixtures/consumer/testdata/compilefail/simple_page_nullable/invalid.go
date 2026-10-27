package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func wrong() {
	p, _ := rows.Read(rows.SelectValue(models.Users.Query(), models.Users.Nickname)).SimplePaginate(nil, nil, orm.PageRequest{})
	var _ orm.SimplePage[string] = p
}
