package invalid

import (
	"foundry.test/consumer/models"
)

var _ = models.UserFields().Age.Param("old")
