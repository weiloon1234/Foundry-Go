package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func wrong(q rows.Query[models.User, reports.UserSummary]) {
	p, _ := rows.Read(q).Paginate(nil, nil, orm.PageRequest{})
	var _ orm.PageResult[models.User] = p
}
