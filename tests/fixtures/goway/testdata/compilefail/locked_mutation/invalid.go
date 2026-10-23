package invalid

import (
	"foundry.test/goway/models"
)

var _, _ = models.Users.Query().ForUpdate().UpdateAll(nil, nil, models.Users.Email.Set("x"))
