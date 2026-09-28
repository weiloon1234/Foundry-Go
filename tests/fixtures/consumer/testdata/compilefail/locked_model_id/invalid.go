package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = models.QueryUsers().ForUpdate().NoWait().Find(nil, nil, model.ID[models.Order]{})
