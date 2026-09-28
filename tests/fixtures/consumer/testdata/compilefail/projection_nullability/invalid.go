package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = query.Map(reports.UserSummaryFields().Email, models.UserFields().Nickname.Value())
