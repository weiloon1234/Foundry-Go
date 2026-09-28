package query

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func executionContext(ctx context.Context, executor database.Executor) error {
	if ctx == nil || executor == nil {
		return fault.New(fault.Invalid, "query execution requires a context and executor")
	}
	return ctx.Err()
}

// Each streams complete models, closing on failure or early callback return.
// Eager relations or possible retrieval hooks use DefaultChunkSize offset
// batches, closing rows before I/O callbacks. Unknown executor wrappers cannot
// prove hooks absent. Batches do not create a shared snapshot; changing selected
// membership/order can shift later results. EachChunked selects an explicit size.
func (q Query[M]) Each(ctx context.Context, executor database.Executor, yield func(M) error) error {
	if len(q.relations) != 0 || q.definition != nil && q.definition.retrieval().mayRun(executor) {
		return q.EachChunked(ctx, executor, DefaultChunkSize, yield)
	}
	return q.eachRows(ctx, executor, yield)
}
func (q Query[M]) eachRows(ctx context.Context, executor database.Executor, yield func(M) error) error {
	if err := executionContext(ctx, executor); err != nil {
		return err
	}
	if yield == nil {
		return fault.New(fault.Invalid, "model iteration requires a callback")
	}
	statement, err := q.Compile()
	if err != nil {
		return err
	}
	return database.ForEach(ctx, readExecutor{executor}, statement.sql, statement.arguments, q.definition.scan, yield)
}

// All collects complete models. On any error it discards the entire partial
// result. Use Limit for bounded collections or Each for streaming workloads.
func (q Query[M]) All(ctx context.Context, executor database.Executor) ([]M, error) {
	result, err := q.allRows(ctx, executor)
	if err != nil {
		return nil, err
	}
	if len(q.relations) == 0 {
		return result, nil
	}
	return q.Load(ctx, executor, result)
}
func (q Query[M]) allRows(ctx context.Context, executor database.Executor) ([]M, error) {
	if err := executionContext(ctx, executor); err != nil {
		return nil, err
	}
	statement, err := q.Compile()
	if err != nil {
		return nil, err
	}
	return collectRead(ctx, executor, statement, q.definition.scan, q.definition.retrieval())
}

func (q Query[M]) atMostOne() Query[M] {
	if limit, set := q.limit.Get(); !set || limit > 1 {
		return q.Limit(1)
	}
	return q
}

func (q Query[M]) firstQuery() Query[M] {
	q = q.atMostOne()
	if len(q.orders) == 0 && q.definition != nil {
		q = q.OrderBy(Order[M]{field: fieldRef{q.table, q.definition.primary}})
	}
	return q
}

// First returns an omitted Optional for no match. It honors filters, offsets and
// a zero limit; absent explicit ordering, primary-key ascending is used.
func (q Query[M]) First(ctx context.Context, executor database.Executor) (value.Optional[M], error) {
	q = q.firstQuery()
	items, err := q.All(ctx, executor)
	if err != nil {
		return value.Optional[M]{}, err
	}
	if len(items) == 0 {
		return value.Optional[M]{}, nil
	}
	return value.Set(items[0]), nil
}

// RequireFirst reports database.NotFound when no model matches the query.
func (q Query[M]) RequireFirst(ctx context.Context, executor database.Executor) (M, error) {
	item, err := q.First(ctx, executor)
	if err != nil {
		return *new(M), err
	}
	model, present := item.Get()
	if !present {
		return *new(M), fmt.Errorf("query first: %w", database.NotFound)
	}
	return model, nil
}

// Count counts the selected query window, including Limit/Offset. It does not
// hydrate models. Query the unpaginated base when a total matching count is needed.
func (q Query[M]) Count(ctx context.Context, executor database.Executor) (int64, error) {
	if err := executionContext(ctx, executor); err != nil {
		return 0, err
	}
	statement, err := q.compile(readCount)
	if err != nil {
		return 0, err
	}
	var count int64
	err = database.ScanOne(ctx, readExecutor{executor}, statement.sql, statement.arguments, &count)
	return count, err
}

// Exists checks whether this query window contains a row, without hydrating it.
func (q Query[M]) Exists(ctx context.Context, executor database.Executor) (bool, error) {
	if err := executionContext(ctx, executor); err != nil {
		return false, err
	}
	statement, err := q.compile(readExists)
	if err != nil {
		return false, err
	}
	var exists bool
	err = database.ScanOne(ctx, readExecutor{executor}, statement.sql, statement.arguments, &exists)
	return exists, err
}
