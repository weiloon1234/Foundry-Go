package invalid

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

var _ = httpdto.UserResponse{ID: model.ID[models.Order]{}}
