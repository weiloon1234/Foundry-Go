package query

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

const MaxPlanBytes = querycore.MaxPlanBytes
const MaxPlanNodes = querycore.MaxPlanNodes
const MaxPlanDepth = querycore.MaxPlanDepth

// PlanNode is a typed snapshot of PostgreSQL's common plan fields. Actual
// measurements are absent for ordinary Explain. Costs are planner units;
// reported time values are milliseconds. JSON retains additional server fields.
type PlanNode = querycore.PlanNode

// Plan owns a bounded response for one compiled SELECT. Inspection does not
// hydrate models, run retrieval hooks or execute eager relation queries. JSON
// may contain predicates and bound values; only explicit accessors expose it.
type Plan = querycore.Plan

type planEnvelope = querycore.PlanEnvelope
type planBytes = querycore.PlanBytes

type planSource interface{ Compile() (Statement, error) }

func explain(ctx context.Context, executor database.Executor, source planSource, analyze bool) (Plan, error) {
	return querycore.Explain(ctx, executor, func(ctx context.Context) (querycore.Statement, error) {
		statement, err := compileSource(ctx, source)
		return statement.coreStatement(), err
	}, analyze)
}

func explainTransaction(ctx context.Context, tx *database.Tx, source planSource, analyze bool) (Plan, error) {
	if err := lockContext(ctx, tx); err != nil {
		return Plan{}, err
	}
	return explain(ctx, tx, source, analyze)
}

// Explain asks PostgreSQL to plan this SELECT without executing it. It reuses
// the exact compiled statement, parameter bindings and supplied executor.
func (q Query[M]) Explain(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, false)
}

// ExplainAnalyze explicitly executes this SELECT and reports runtime statistics.
// Use the transaction-specific variants for statements containing row locks.
func (q Query[M]) ExplainAnalyze(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, true)
}

func (q ProjectionQuery[S, P]) Explain(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, false)
}

func (q ProjectionQuery[S, P]) ExplainAnalyze(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, true)
}

func (q SetQuery[R]) Explain(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, false)
}

func (q SetQuery[R]) ExplainAnalyze(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, true)
}

func (q ValueQuery[S, V]) Explain(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, false)
}

func (q ValueQuery[S, V]) ExplainAnalyze(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, true)
}

func (q ValueSetQuery[V]) Explain(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, false)
}

func (q ValueSetQuery[V]) ExplainAnalyze(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, true)
}

// Explain plans the canonical cursor result without inventing a page boundary.
func (q CursorQuery[R]) Explain(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, false)
}

func (q CursorQuery[R]) ExplainAnalyze(ctx context.Context, executor database.Executor) (Plan, error) {
	return explain(ctx, executor, q, true)
}

// Explain and ExplainAnalyze retain the transaction capability of their source.
// Planning does not take the SELECT's row locks; analysis takes them for the
// actual transaction and leaves commit/rollback ownership with the caller.
func (q LockedQuery[M]) Explain(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, false)
}

func (q LockedQuery[M]) ExplainAnalyze(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, true)
}

func (q LockedResult[S, R]) Explain(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, false)
}

func (q LockedResult[S, R]) ExplainAnalyze(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, true)
}

func (q TransactionQuery[S, R]) Explain(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, false)
}

func (q TransactionQuery[S, R]) ExplainAnalyze(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, true)
}

func (q TransactionValueQuery[S, V]) Explain(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, false)
}

func (q TransactionValueQuery[S, V]) ExplainAnalyze(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, true)
}

func (q LockedTransactionValue[S, V]) Explain(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, false)
}

func (q LockedTransactionValue[S, V]) ExplainAnalyze(ctx context.Context, tx *database.Tx) (Plan, error) {
	return explainTransaction(ctx, tx, q, true)
}
