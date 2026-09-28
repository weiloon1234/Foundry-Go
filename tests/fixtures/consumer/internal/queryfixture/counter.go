package queryfixture

import (
	"context"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/database"
)

// QueryCounter records SQL reads through an existing fixture executor.
type QueryCounter struct {
	database.Executor
	Queries atomic.Int64
}

func (c *QueryCounter) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	c.Queries.Add(1)
	return c.Executor.Query(ctx, sql, args...)
}
