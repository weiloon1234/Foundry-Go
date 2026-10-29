package locked_pool

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func F() {
	var ex database.Executor
	orm.AllLocked(context.Background(), ex, orm.Lock(models.Orders.Query()))
}
