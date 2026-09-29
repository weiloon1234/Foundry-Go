package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxPlanBytes = 4 << 20
const MaxPlanNodes = 4096
const MaxPlanDepth = 24

// PlanNode is a typed snapshot of PostgreSQL's common plan fields. Actual
// measurements are absent for ordinary Explain. Costs are planner units;
// reported time values are milliseconds. JSON retains additional server fields.
type PlanNode struct {
	NodeType           string                  `json:"Node Type"`
	ParentRelationship string                  `json:"Parent Relationship,omitempty"`
	RelationName       string                  `json:"Relation Name,omitempty"`
	Schema             string                  `json:"Schema,omitempty"`
	Alias              string                  `json:"Alias,omitempty"`
	IndexName          string                  `json:"Index Name,omitempty"`
	JoinType           string                  `json:"Join Type,omitempty"`
	Strategy           string                  `json:"Strategy,omitempty"`
	ParallelAware      bool                    `json:"Parallel Aware"`
	StartupCost        float64                 `json:"Startup Cost"`
	TotalCost          float64                 `json:"Total Cost"`
	PlanRows           float64                 `json:"Plan Rows"`
	PlanWidth          int64                   `json:"Plan Width"`
	ActualStartupTime  value.Optional[float64] `json:"Actual Startup Time,omitzero"`
	ActualTotalTime    value.Optional[float64] `json:"Actual Total Time,omitzero"`
	ActualRows         value.Optional[float64] `json:"Actual Rows,omitzero"`
	ActualLoops        value.Optional[float64] `json:"Actual Loops,omitzero"`
	Plans              []PlanNode              `json:"Plans,omitempty"`
}

type planEnvelope struct {
	Plan          PlanNode                `json:"Plan"`
	PlanningTime  value.Optional[float64] `json:"Planning Time,omitzero"`
	ExecutionTime value.Optional[float64] `json:"Execution Time,omitzero"`
}

// Plan owns a bounded response for one compiled SELECT. Inspection does not
// hydrate models, run retrieval hooks or execute eager relation queries. JSON
// may contain predicates and bound values; only explicit accessors expose it.
type Plan struct {
	document planEnvelope
	json     []byte
	analyzed bool
	nodes    int
}

func (p Plan) Analyzed() bool                                 { return p.analyzed }
func (p Plan) NodeCount() int                                 { return p.nodes }
func (p Plan) Root() PlanNode                                 { return clonePlanNode(p.document.Plan) }
func (p Plan) PlanningMilliseconds() value.Optional[float64]  { return p.document.PlanningTime }
func (p Plan) ExecutionMilliseconds() value.Optional[float64] { return p.document.ExecutionTime }
func (p Plan) JSON() []byte                                   { return slices.Clone(p.json) }
func (p Plan) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprintf(state, "query plan (nodes %d, analyzed %t)", p.nodes, p.analyzed)
}
func clonePlanNode(node PlanNode) PlanNode {
	node.Plans = slices.Clone(node.Plans)
	for i := range node.Plans {
		node.Plans[i] = clonePlanNode(node.Plans[i])
	}
	return node
}

type planSource interface{ Compile() (Statement, error) }

func explain(ctx context.Context, executor database.Executor, source planSource, analyze bool) (Plan, error) {
	if err := executionContext(ctx, executor); err != nil {
		return Plan{}, err
	}
	var result Plan
	err := callback.Isolated("inspect query plan", func() error {
		statement, err := compileSource(ctx, source)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		prefix := "EXPLAIN (FORMAT JSON, ANALYZE FALSE, VERBOSE FALSE, COSTS TRUE) "
		if analyze {
			prefix = "EXPLAIN (FORMAT JSON, ANALYZE TRUE, VERBOSE FALSE, COSTS TRUE, BUFFERS TRUE, TIMING TRUE) "
		}
		var response planBytes
		if err := database.ScanOne(ctx, readExecutor{executor}, prefix+statement.sql, statement.arguments, &response); err != nil {
			return err
		}
		result, err = decodePlan(ctx, response.data, analyze)
		return err
	})
	if err != nil {
		return Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	return result, nil
}
func explainTransaction(ctx context.Context, tx *database.Tx, source planSource, analyze bool) (Plan, error) {
	if err := lockContext(ctx, tx); err != nil {
		return Plan{}, err
	}
	return explain(ctx, tx, source, analyze)
}

type planBytes struct{ data []byte }

func (p *planBytes) Scan(source any) error {
	var data []byte
	switch source := source.(type) {
	case []byte:
		if len(source) == 0 || len(source) > MaxPlanBytes {
			return invalidPlan()
		}
		data = slices.Clone(source)
	case string:
		if len(source) == 0 || len(source) > MaxPlanBytes {
			return invalidPlan()
		}
		data = []byte(source)
	default:
		return invalidPlan()
	}
	p.data = data
	return nil
}
func invalidPlan() error {
	return fault.New(fault.Invalid, "invalid or oversized PostgreSQL query plan")
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
