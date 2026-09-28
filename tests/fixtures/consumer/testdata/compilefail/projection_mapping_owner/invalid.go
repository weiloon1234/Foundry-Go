package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.Project(models.QueryUsers(), reports.UserSummaryProjection(), query.Map(reports.BuyerTotalsFields().Orders, query.Count[models.User]().Value()))
