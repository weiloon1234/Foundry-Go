package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}
type other struct{}

func invalid() {
	_ = reports.ProjectScalarReport(models.QueryUsers()).SelectBuyerID(query.ScalarQuery(models.QueryOrders(), query.SelectValue(models.QueryUsers(), models.UserFields().ID.Value())))
}
