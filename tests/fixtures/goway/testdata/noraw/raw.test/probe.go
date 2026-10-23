package rawtest

import (
	"context"
	db "github.com/weiloon1234/Foundry-Go/database"
)

func Run(ctx context.Context, ex db.Executor) {
	_, _ = ex.Query(ctx, "private_sql_canary") // want database.Query
}
