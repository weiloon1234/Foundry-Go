package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = models.QueryUsers().Where(query.RowNumber(query.WindowFor(models.QueryUsers())))
