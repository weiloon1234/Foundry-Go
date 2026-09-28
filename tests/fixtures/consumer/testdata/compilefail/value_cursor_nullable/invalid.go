package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	c := query.ValueCursorFor(query.SelectValue(models.QueryUsers(), models.UserFields().Nickname.Value()))
	var page query.CursorPage[string]
	page, _ = c.Paginate(nil, nil, query.CursorRequest[string]{Size: 1})
	_ = page
}
