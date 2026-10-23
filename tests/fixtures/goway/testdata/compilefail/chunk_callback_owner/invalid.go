package invalid

import (
	"foundry.test/goway/models"
)

func wrong() { _ = models.Users.Query().Chunk(nil, nil, 2, func([]models.Order) error { return nil }) }
