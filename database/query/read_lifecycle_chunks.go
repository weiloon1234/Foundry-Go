package query

import (
	"context"
	"database/sql/driver"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func (q readResult[R]) retrievalChunks(ctx context.Context, executor database.Executor, yield func(R) error) error {
	var items []R
	var key func() (driver.Value, error)
	if q.lifecycle.primaryKey != nil {
		key = func() (driver.Value, error) { return q.lifecycle.primaryKey(items[len(items)-1]) }
	}
	return ormcore.RecordBatches(ctx, q.node, q.lifecycle.primaryIndex, func(node querycore.SelectNode) error {
		q.node = node
		_, err := q.Compile()
		return err
	}, func(node querycore.SelectNode) (int, error) {
		q.node = node
		var err error
		items, err = q.All(ctx, executor)
		return len(items), err
	}, key, func(i int) error { return yield(items[i]) })
}
