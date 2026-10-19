package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	foundryquery "github.com/weiloon1234/Foundry-Go/database/query"
)

var invalid = reports.UserSummarySelection[models.User]{Email: foundryquery.Value(models.UserFields().Age)}
