package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	link := query.Correlate(models.QueryUsers(), models.QueryOrders())
	_ = query.SelectValue(link, models.OrderFields().ID.Value())
}
