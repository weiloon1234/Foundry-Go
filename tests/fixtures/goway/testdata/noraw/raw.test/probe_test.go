package rawtest_test

import (
	"context"
	db "github.com/weiloon1234/Foundry-Go/database"
)

func Reference(ctx context.Context, ex db.Executor) {
	_, _ = ex.Exec(ctx, "private_sql_canary") // want database.Exec
}
