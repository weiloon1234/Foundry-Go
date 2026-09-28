package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = query.SelectValue(models.QueryUsers(), query.Count[models.User]().Value()).GroupBy(query.Count[models.User]().Value().Key())
