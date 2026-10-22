package enum_ordered

import (
	"foundry.test/goway/models"
)

func F() { models.Users.Status.Gt(models.StatusActive) }
