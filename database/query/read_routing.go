package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
)

// This adapter is used only where the query AST has already established SELECT
// semantics. Raw SQL helpers, mutations and transaction ownership stay intact.
// Hook callbacks receive their original executor, not this temporary adapter.
type readExecutor struct{ database.Executor }

func (e readExecutor) Query(ctx context.Context, statement string, arguments ...any) (*database.Rows, error) {
	return database.ReadQuery(ctx, e.Executor, statement, arguments...)
}
