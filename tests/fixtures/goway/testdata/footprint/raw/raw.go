package raw

import (
	"context"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Models(ctx context.Context, ex database.Executor) ([]models.Order, error) {
	return models.Orders.Raw(ctx, ex, "SELECT * FROM orders")
}

type Summary struct{ ID int64 }

var schema = rows.Define(func(b rows.Builder[Summary], r *Summary) rows.Row[Summary] { rows.NumberOf(b, &r.ID); return b.Row() })

func Summaries(ctx context.Context, ex database.Executor) ([]Summary, error) {
	return rows.Raw(ctx, ex, schema, "SELECT id FROM orders")
}
