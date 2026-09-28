package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func invalid() {
	_ = models.UserFields().ID.EqColumn(models.OrderFields().BuyerID)
}
