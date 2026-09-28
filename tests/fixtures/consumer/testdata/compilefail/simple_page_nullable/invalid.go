package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func wrong() {
	p, _ := query.SelectValue(models.QueryUsers(), models.UserFields().Nickname.Value()).SimplePaginate(nil, nil, query.PageRequest{})
	var _ query.SimplePage[string] = p
}
