package rows

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// CompileCursor validates output keys and compiles canonical ordering without I/O.
// It adds no page boundary or lookahead limit. Example: rows.CompileCursor(cursor).
func CompileCursor[R any](q CursorQuery[R]) (orm.Statement, error) {
	return compileCursorContext(nil, q.core)
}

// PreflightCursor validates a cursor with the executing request context and no I/O.
// Example: rows.PreflightCursor(ctx,cursor).
func PreflightCursor[R any](ctx context.Context, q CursorQuery[R]) error {
	return preflightCursor(ctx, q.core)
}

// CompileValueCursor inspects a scalar cursor without a page boundary or I/O.
// Example: rows.CompileValueCursor(cursor).
func CompileValueCursor[V any](q ValueCursorQuery[V]) (orm.Statement, error) {
	return compileCursorContext(nil, q.core)
}

// PreflightValueCursor validates a scalar cursor with the executing request context.
// Example: rows.PreflightValueCursor(ctx,cursor).
func PreflightValueCursor[V any](ctx context.Context, q ValueCursorQuery[V]) error {
	return preflightCursor(ctx, q.core)
}

func preflightCursor(ctx context.Context, q ormcore.ResultCursor) error {
	if ctx == nil {
		return ormcore.Invalid("query execution requires a context")
	}
	_, err := compileCursorContext(ctx, q)
	return err
}
func compileCursorContext(ctx context.Context, q ormcore.ResultCursor) (orm.Statement, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return orm.Statement{}, err
		}
	}
	if q.Record.Transaction {
		return orm.Statement{}, ormcore.Invalid("ordinary cursor requires an ordinary source")
	}
	statement, err := ormcore.CompileResultCursor(ctx, q)
	if err != nil {
		return orm.Statement{}, err
	}
	return orm.FoundryStatement(statement, sqlowner.Metadata())
}
