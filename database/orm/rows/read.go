package rows

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Compile inspects a projection without I/O; bound scope contexts are preserved. Example: rows.Compile(q).
func Compile[S, R any](q Query[S, R]) (orm.Statement, error) {
	return compileContext(nil, q)
}
func compileContext[S, R any](ctx context.Context, q Query[S, R]) (orm.Statement, error) {
	return compileReadContext(ctx, Read(q).core)
}

// Preflight validates a projection with the executing request context and no I/O. Example: rows.Preflight(ctx,q).
func Preflight[S, R any](ctx context.Context, q Query[S, R]) error {
	if ctx == nil {
		return ormcore.Invalid("query execution requires a context")
	}
	_, err := compileContext(ctx, q)
	return err
}

// Count counts complete projected results, including grouping and existing windows. Example: rows.Count(ctx,db,q).
func Count[S, R any](ctx context.Context, ex database.Executor, q Query[S, R]) (int64, error) {
	return Read(q).Count(ctx, ex)
}

// Each streams one decoded result at a time and closes rows on every exit. Example: rows.Each(ctx,tx,q,writeCSV).
func Each[S, R any](ctx context.Context, ex database.Executor, q Query[S, R], yield func(R) error) error {
	err := q.source.Err
	if yield == nil {
		err = errors.Join(err, ormcore.Invalid("query iteration requires a callback"))
	}
	if q.decode == nil {
		err = errors.Join(err, ormcore.Invalid("query requires a complete result decoder"))
	}
	return ormcore.Read(ctx, ex, q.source.Node, err, q.source.Checks, func(row database.Row) error {
		item, err := q.decode(row)
		if err != nil {
			return err
		}
		return ormcore.Iteration(ctx, func() error { return yield(item) })
	})
}

// ReorderUnwindowed replaces report ordering, rejecting outer windows and DISTINCT ON. Example: rows.ReorderUnwindowed(q,name.Asc(),id.Asc()).
func ReorderUnwindowed[S, R any](q Query[S, R], orders ...expr.ProjectionOrder[S]) Query[S, R] {
	node, err := querycore.ReorderUnwindowed(q.source.Node, len(orders))
	if err != nil {
		q.source.Err = errors.Join(q.source.Err, err)
		return q
	}
	q.source.Node = node
	return OrderBy(q, orders...)
}
