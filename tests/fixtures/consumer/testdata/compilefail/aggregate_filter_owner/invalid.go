package invalid

import (
	"foundry.test/consumer/models"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = foundryquery.Sum(models.OrderFields().TotalCents).Filter(models.UserFields().Age.Gt(18))
