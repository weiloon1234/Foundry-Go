package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	link := query.Correlate(models.QueryUsers(), models.QueryOrders())
	u := models.UserFieldsAt(query.OuterScope(link, models.QueryUsers().Scope()))
	o := models.OrderFieldsAt(query.InnerScope(link, models.QueryOrders().Scope()))
	_ = u.ID.EqColumn(o.ID)
}
