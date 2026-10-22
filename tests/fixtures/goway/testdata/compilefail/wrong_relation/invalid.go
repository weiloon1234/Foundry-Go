package wrong_relation

import (
	"foundry.test/goway/models"
)

func F() { models.Orders.Query().With(models.UserRelations.Orders) }
