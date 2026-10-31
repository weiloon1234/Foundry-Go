package rows

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// streamRows streams one decoded result at a time and closes rows on every exit.
func streamRows[R any](ctx context.Context, ex database.Executor, source ormcore.Source, decode func(database.Row) (R, error), yield func(R) error) error {
	err := source.Err
	if yield == nil {
		err = safeerror.Join("operation failed", err, ormcore.Invalid("query iteration requires a callback"))
	}
	if decode == nil {
		err = safeerror.Join("operation failed", err, ormcore.Invalid("query requires a complete result decoder"))
	}
	return ormcore.Read(ctx, ex, source.Node, err, source.Checks, func(row database.Row) error {
		item, err := decode(row)
		if err != nil {
			return err
		}
		return ormcore.Iteration(ctx, func() error { return yield(item) })
	})
}

// ReorderUnwindowed replaces report ordering, rejecting outer windows and DISTINCT ON. Example: rows.ReorderUnwindowed(q,expr.Asc(name),expr.Asc(id)).
func ReorderUnwindowed[S, R any](q Query[S, R], orders ...expr.ProjectionOrder[S]) Query[S, R] {
	node, err := querycore.ReorderUnwindowed(q.source.Node, len(orders))
	if err != nil {
		q.source.Err = safeerror.Join("operation failed", q.source.Err, err)
		return q
	}
	q.source.Node = node
	return OrderBy(q, orders...)
}
