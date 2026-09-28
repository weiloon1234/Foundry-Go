package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func wrong(q query.ProjectionQuery[models.User, reports.UserSummary]) {
	p, _ := q.Paginate(nil, nil, query.PageRequest{})
	var _ query.Page[models.User] = p
}
