package invalid

import (
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/models"
)

var _ = httpdto.UserResponse{State: models.LevelBasic}
