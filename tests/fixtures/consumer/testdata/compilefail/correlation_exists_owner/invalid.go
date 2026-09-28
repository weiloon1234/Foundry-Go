package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = models.QueryOrders().Where(query.Correlate(models.QueryUsers(), models.QueryOrders()).Exists())
}
