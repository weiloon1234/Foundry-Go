package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = models.Users.Query().ForUpdate().Update(nil, nil, model.ID[models.User]{}, models.Users.Age.Set(20))
