package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _, _ = models.QueryUsers().ForUpdate().Update(nil, nil, model.ID[models.User]{}, models.UserDraft{})
