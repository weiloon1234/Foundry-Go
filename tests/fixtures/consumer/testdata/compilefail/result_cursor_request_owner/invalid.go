package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	c := query.CursorFor(models.QueryUsers())
	_, _ = c.Paginate(nil, nil, query.CursorRequest[models.Order]{Size: 1})
}
