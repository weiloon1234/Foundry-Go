package rows

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Explain plans an ordinary projection, scalar or set adapted through Read.
// Example: rows.Explain(ctx, db, rows.Read(report)).
func Explain[S, R any](ctx context.Context, ex database.Executor, q Result[S, R]) (orm.Plan, error) {
	return explainRecord(ctx, ex, q.core, false, false)
}

// ExplainAnalyze executes an ordinary result and reports actual measurements.
// Example: rows.ExplainAnalyze(ctx, db, rows.ReadSet(combined)).
func ExplainAnalyze[S, R any](ctx context.Context, ex database.Executor, q Result[S, R]) (orm.Plan, error) {
	return explainRecord(ctx, ex, q.core, true, false)
}

// ExplainTransaction plans records retaining nested transaction requirements.
// Example: rows.ExplainTransaction(ctx, tx, rows.LockedRecord(Users.Query().ForUpdate())).
func ExplainTransaction[R any](ctx context.Context, tx *database.Tx, q TransactionRecordSource[R]) (orm.Plan, error) {
	return explainRecord(ctx, tx, transactionRecordOf(q), false, true)
}

// ExplainAnalyzeTransaction executes transaction-owned records.
// Example: rows.ExplainAnalyzeTransaction(ctx, tx, selected).
func ExplainAnalyzeTransaction[R any](ctx context.Context, tx *database.Tx, q TransactionRecordSource[R]) (orm.Plan, error) {
	return explainRecord(ctx, tx, transactionRecordOf(q), true, true)
}

// ExplainTransactionValue plans a transaction-owned scalar.
// Example: rows.ExplainTransactionValue(ctx, tx, selected).
func ExplainTransactionValue[S, V any](ctx context.Context, tx *database.Tx, q TransactionValue[S, V]) (orm.Plan, error) {
	return explainRecord(ctx, tx, q.core, false, true)
}

// ExplainAnalyzeTransactionValue executes a transaction-owned scalar.
// Example: rows.ExplainAnalyzeTransactionValue(ctx, tx, selected).
func ExplainAnalyzeTransactionValue[S, V any](ctx context.Context, tx *database.Tx, q TransactionValue[S, V]) (orm.Plan, error) {
	return explainRecord(ctx, tx, q.core, true, true)
}
func explainRecord(ctx context.Context, ex database.Executor, r ormcore.RecordSource, analyze, transaction bool) (orm.Plan, error) {
	if err := ormcore.RecordExecution(ctx, ex, transaction); err != nil {
		return orm.Plan{}, err
	}
	if r.Transaction != transaction {
		return orm.Plan{}, ormcore.Invalid("query plan executor kind does not match its source")
	}
	return querycore.Explain(ctx, ex, func(ctx context.Context) (querycore.Statement, error) { return ormcore.CompileRecord(ctx, r) }, analyze)
}

// ExplainCursor plans a canonical completed cursor without a page boundary.
// Example: rows.ExplainCursor(ctx, db, cursor).
func ExplainCursor[R any](ctx context.Context, ex database.Executor, q CursorQuery[R]) (orm.Plan, error) {
	return explainCursor(ctx, ex, q.core, false)
}

// ExplainAnalyzeCursor executes the canonical cursor result without pagination.
// Example: rows.ExplainAnalyzeCursor(ctx, db, cursor).
func ExplainAnalyzeCursor[R any](ctx context.Context, ex database.Executor, q CursorQuery[R]) (orm.Plan, error) {
	return explainCursor(ctx, ex, q.core, true)
}

// ExplainValueCursor plans a canonical scalar cursor without a page boundary.
// Example: rows.ExplainValueCursor(ctx, db, cursor).
func ExplainValueCursor[V any](ctx context.Context, ex database.Executor, q ValueCursorQuery[V]) (orm.Plan, error) {
	return explainCursor(ctx, ex, q.core, false)
}

// ExplainAnalyzeValueCursor executes the canonical scalar cursor result.
// Example: rows.ExplainAnalyzeValueCursor(ctx, db, cursor).
func ExplainAnalyzeValueCursor[V any](ctx context.Context, ex database.Executor, q ValueCursorQuery[V]) (orm.Plan, error) {
	return explainCursor(ctx, ex, q.core, true)
}
func explainCursor(ctx context.Context, ex database.Executor, q ormcore.ResultCursor, analyze bool) (orm.Plan, error) {
	if err := ormcore.RecordExecution(ctx, ex, false); err != nil {
		return orm.Plan{}, err
	}
	if q.Record.Transaction {
		return orm.Plan{}, ormcore.Invalid("cursor plans require an ordinary source")
	}
	return querycore.Explain(ctx, ex, func(ctx context.Context) (querycore.Statement, error) { return ormcore.CompileResultCursor(ctx, q) }, analyze)
}
