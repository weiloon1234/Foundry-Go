package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryExplain shares bounded plan execution and decoding with framework query frontends.
// Example: FoundryExplain(ctx, tx, compile, true, sqlowner.Queries()).
func FoundryExplain(ctx context.Context, executor database.Executor, compile func(context.Context) (querycore.Statement, error), analyze bool, seal sqlowner.QuerySeal) (Plan, error) {
	if !seal.Valid() || compile == nil {
		return Plan{}, fault.New(fault.Invalid, "invalid query plan capability")
	}
	return querycore.Explain(ctx, executor, compile, analyze)
}
