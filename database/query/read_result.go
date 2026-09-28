package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// readResult shares complete-record execution across projections and set queries.
type readResult[R any] struct {
	node        selectNode
	scan        func(database.Row) (R, error)
	err         error
	transaction bool
	lifecycle   *readLifecycle[R]
}

func (q readResult[R]) Compile() (Statement, error) {
	if q.err != nil {
		return Statement{}, q.err
	}
	if q.scan == nil {
		return Statement{}, fault.New(fault.Invalid, "query requires a complete result decoder")
	}
	c := compiler{allowLocks: q.transaction}
	sql, err := c.compileSelect(q.node)
	if err != nil {
		return Statement{}, err
	}
	return Statement{sql: sql, arguments: c.arguments}, nil
}
func (q readResult[P]) Each(ctx context.Context, executor database.Executor, yield func(P) error) error {
	if err := executionContext(ctx, executor); err != nil {
		return err
	}
	if yield == nil {
		return fault.New(fault.Invalid, "query iteration requires a callback")
	}
	if q.lifecycle.mayRun(executor) {
		return q.retrievalChunks(ctx, executor, yield)
	}
	s, err := q.Compile()
	if err != nil {
		return err
	}
	return database.ForEach(ctx, readExecutor{executor}, s.sql, s.arguments, q.scan, yield)
}
func (q readResult[P]) All(ctx context.Context, executor database.Executor) ([]P, error) {
	if err := executionContext(ctx, executor); err != nil {
		return nil, err
	}
	s, err := q.Compile()
	if err != nil {
		return nil, err
	}
	return collectRead(ctx, executor, s, q.scan, q.lifecycle)
}
func (q readResult[P]) First(ctx context.Context, executor database.Executor) (value.Optional[P], error) {
	if n, set := q.node.limit.Get(); !set || n > 1 {
		q.node.limit = value.Set(1)
	}
	rows, err := q.All(ctx, executor)
	if err != nil {
		return value.Optional[P]{}, err
	}
	if len(rows) == 0 {
		return value.Optional[P]{}, nil
	}
	return value.Set(rows[0]), nil
}
func (q readResult[P]) RequireFirst(ctx context.Context, executor database.Executor) (P, error) {
	row, err := q.First(ctx, executor)
	if err != nil {
		return *new(P), err
	}
	if p, ok := row.Get(); ok {
		return p, nil
	}
	return *new(P), database.NotFound
}

// Count counts the selected result window, including grouped rows and pagination.
func (q readResult[P]) Count(ctx context.Context, executor database.Executor) (int64, error) {
	if err := executionContext(ctx, executor); err != nil {
		return 0, err
	}
	s, err := q.Compile()
	if err != nil {
		return 0, err
	}
	var n int64
	err = database.ScanOne(ctx, readExecutor{executor}, `SELECT COUNT(*) FROM (`+s.sql+`) AS "foundry_count"`, s.arguments, codec.Signed[int64]().Scan(&n))
	return n, err
}
func (q readResult[P]) Exists(ctx context.Context, executor database.Executor) (bool, error) {
	if err := executionContext(ctx, executor); err != nil {
		return false, err
	}
	if n, set := q.node.limit.Get(); !set || n > 1 {
		q.node.limit = value.Set(1)
	}
	s, err := q.Compile()
	if err != nil {
		return false, err
	}
	var exists bool
	err = database.ScanOne(ctx, readExecutor{executor}, "SELECT EXISTS("+s.sql+")", s.arguments, codec.Bool[bool]().Scan(&exists))
	return exists, err
}
