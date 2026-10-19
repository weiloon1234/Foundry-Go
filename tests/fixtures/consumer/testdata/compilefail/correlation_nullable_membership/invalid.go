package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	link := query.Correlate(models.QueryUsers(), models.QueryOrders())
	buyer := models.UserFieldsAt(query.OuterScope(link, models.QueryUsers().Scope()))
	order := models.OrderFieldsAt(query.InnerScope(link, models.QueryOrders().Scope()))
	values := query.SelectCorrelatedValue(link, query.Value(order.BuyerID)).Where(order.BuyerID.EqColumn(buyer.ID))
	nullable := query.SelectCorrelatedValue(link, query.Nullable(query.Value(order.BuyerID)))
	_ = values
	_ = models.UserFields().ID.InCorrelatedQuery(nullable)
}
