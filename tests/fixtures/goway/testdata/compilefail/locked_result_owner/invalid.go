package invalid

import (
	"foundry.test/goway/models"
)

func invalid() ([]models.Order, error) { return models.Users.Query().ForUpdate().All(nil, nil) }
