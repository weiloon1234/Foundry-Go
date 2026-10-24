package querycore

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
	"github.com/weiloon1234/Foundry-Go/value"
	"slices"
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

type PlanEnvelope struct {
	Plan          PlanNode                `json:"Plan"`
	PlanningTime  value.Optional[float64] `json:"Planning Time,omitzero"`
	ExecutionTime value.Optional[float64] `json:"Execution Time,omitzero"`
}

// Plan owns a bounded response for one compiled SELECT. Inspection does not
// hydrate models, run retrieval hooks or execute eager relation queries. JSON
// may contain predicates and bound values; only explicit accessors expose it.
type Plan struct {
	document PlanEnvelope
	json     []byte
	analyzed bool
	nodes    int
}

func (p Plan) Analyzed() bool { return p.analyzed }

func (p Plan) NodeCount() int { return p.nodes }

func (p Plan) Root() PlanNode { return clonePlanNode(p.document.Plan) }

func (p Plan) PlanningMilliseconds() value.Optional[float64] { return p.document.PlanningTime }

func (p Plan) ExecutionMilliseconds() value.Optional[float64] { return p.document.ExecutionTime }

func (p Plan) JSON() []byte { return slices.Clone(p.json) }

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

type PlanBytes struct{ data []byte }

func (p *PlanBytes) Scan(source any) error {
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

// Explain shares bounded plan execution across typed query frontends.
func Explain(ctx context.Context, executor database.Executor, compile func(context.Context) (Statement, error), analyze bool) (Plan, error) {
	if ctx == nil || executor == nil {
		return Plan{}, fault.New(fault.Invalid, "query execution requires a context and executor")
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	if compile == nil {
		return Plan{}, fault.New(fault.Invalid, "invalid query plan capability")
	}
	var result Plan
	ctx = statementmeta.CaptureSite(ctx)
	err := callback.Isolated("inspect query plan", func() error {
		statement, err := compile(ctx)
		if err != nil {
			return lifecore.Failure("query plan compilation failed", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		prefix := "EXPLAIN (FORMAT JSON, ANALYZE FALSE, VERBOSE FALSE, COSTS TRUE) "
		if analyze {
			prefix = "EXPLAIN (FORMAT JSON, ANALYZE TRUE, VERBOSE FALSE, COSTS TRUE, BUFFERS TRUE, TIMING TRUE) "
		}
		var response PlanBytes
		statement = statement.Envelope(prefix, "")
		if err := database.ScanOne(statement.Context(ctx), planReadExecutor{executor}, statement.SQLText, statement.Bindings, &response); err != nil {
			return lifecore.Failure("query plan execution failed", err)
		}
		result, err = DecodePlan(ctx, response.data, analyze)
		return lifecore.Failure("query plan decoding failed", err)
	})
	if err != nil {
		return Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	return result, nil
}

type planReadExecutor struct{ database.Executor }

func (e planReadExecutor) Query(ctx context.Context, sql string, args ...any) (*database.Rows, error) {
	return database.ReadQuery(ctx, e.Executor, sql, args...)
}
