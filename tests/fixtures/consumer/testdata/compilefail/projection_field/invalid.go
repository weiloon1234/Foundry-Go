package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
)

var invalid = reports.UserSummarySelection[models.User]{Email: models.UserFields().Age.Value()}
