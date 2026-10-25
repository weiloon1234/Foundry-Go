package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _, _ = models.Users.Query().ForUpdate().All(nil, (*database.DB)(nil))
