package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Plan owns bounded PostgreSQL plan data; formatting omits predicates and values.
// Example: plan.Root().TotalCost.
type Plan = querycore.Plan

// PlanNode contains typed planner statistics. Example: plan.Root().NodeType.
type PlanNode = querycore.PlanNode

// Explain plans a SELECT without hydration, retrieval callbacks or eager loads.
// Example: orm.Explain(ctx, db, Users.Query()).
func Explain[M any](ctx context.Context, ex database.Executor, q Query[M]) (Plan, error) {
	return explainModel(ctx, ex, q.state, false)
}

// ExplainAnalyze executes the SELECT and includes actual measurements.
// Example: orm.ExplainAnalyze(ctx, db, Users.Query().Limit(10)).
func ExplainAnalyze[M any](ctx context.Context, ex database.Executor, q Query[M]) (Plan, error) {
	return explainModel(ctx, ex, q.state, true)
}

// ExplainLocked plans a locked SELECT using its required transaction.
// Example: orm.ExplainLocked(ctx, tx, Users.Query().ForUpdate()).
func ExplainLocked[M any](ctx context.Context, tx *database.Tx, q LockedQuery[M]) (Plan, error) {
	if err := lockContext(ctx, tx); err != nil {
		return Plan{}, err
	}
	return explainModel(ctx, tx, q.state, false)
}

// ExplainAnalyzeLocked takes the SELECT locks in the caller's transaction.
// Example: orm.ExplainAnalyzeLocked(ctx, tx, Users.Query().ForUpdate()).
func ExplainAnalyzeLocked[M any](ctx context.Context, tx *database.Tx, q LockedQuery[M]) (Plan, error) {
	if err := lockContext(ctx, tx); err != nil {
		return Plan{}, err
	}
	return explainModel(ctx, tx, q.state, true)
}
func explainModel(ctx context.Context, ex database.Executor, q queryState, analyze bool) (Plan, error) {
	return querycore.Explain(ctx, ex, func(ctx context.Context) (querycore.Statement, error) {
		q.ast.ScopeContext = ctx
		return compileRead(q, querycore.ReadModels)
	}, analyze)
}
