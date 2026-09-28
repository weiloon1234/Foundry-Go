package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

var _, _ = models.QueryUsers().ForUpdate().All(nil, (*database.DB)(nil))
