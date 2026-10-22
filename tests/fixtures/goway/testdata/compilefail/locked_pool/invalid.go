package locked_pool

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F() { var ex database.Executor; models.Orders.Query().ForUpdate().All(context.Background(), ex) }
