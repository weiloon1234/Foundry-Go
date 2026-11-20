package key_chunk

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func Chunk(ctx context.Context, db database.Executor, visit func([]models.Order) error) error {
	return orm.ChunkByKey(ctx, db, models.Orders.Query(), models.Orders.ID, 500, visit)
}
func Each(ctx context.Context, db database.Executor, visit func(models.Order) error) error {
	return orm.EachByKey(ctx, db, models.Orders.Query(), models.Orders.ID, 500, visit)
}
