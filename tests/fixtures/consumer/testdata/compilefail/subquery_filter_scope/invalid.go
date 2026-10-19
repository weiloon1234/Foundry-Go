package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

type alias struct{}
type other struct{}

func invalid() {
	_ = query.SelectValue(models.QueryOrders(), query.Value(models.OrderFields().ID)).Where(models.UserFields().Age.Gt(18))
}
