package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.WindowFor(models.QueryUsers()).OrderBy(models.OrderFields().TotalCents.Desc())
