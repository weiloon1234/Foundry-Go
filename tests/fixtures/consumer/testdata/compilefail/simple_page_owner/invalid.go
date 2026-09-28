package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func wrong() {
	p, _ := models.QueryUsers().SimplePaginate(nil, nil, query.PageRequest{})
	var _ query.SimplePage[models.Order] = p
}
