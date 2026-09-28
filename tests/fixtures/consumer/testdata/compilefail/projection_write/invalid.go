package invalid

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
)

var invalid = reports.SelectUserSummary(models.QueryUsers(), reports.UserSummarySelection[models.User]{}).Create
