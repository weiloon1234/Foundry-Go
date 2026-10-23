package invalid

import (
	"foundry.test/goway/models"
)

var _, _ = models.Users.Query().ForUpdate().Count(nil, nil)
