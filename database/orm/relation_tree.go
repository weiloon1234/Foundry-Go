package orm

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// RelationLimits is the existing shared public limits type. MaxRows separately
// bounds fetched rows and attached values across the whole tree.
// Example: limits := orm.DefaultRelationLimits().
type RelationLimits = querycore.RelationLimits

// DefaultRelationLimits uses the shared defaults: batches 500, rows 10000, depth 8.
// Example: limits := orm.DefaultRelationLimits().
func DefaultRelationLimits() RelationLimits { return querycore.DefaultRelationLimits() }

// WithRelationLimits returns a query with immutable tree-wide limits.
// Example: q = orm.WithRelationLimits(q, orm.RelationLimits{BatchSize:100,MaxRows:5000,MaxDepth:4}).
func WithRelationLimits[M any](q Query[M], limits RelationLimits) Query[M] {
	q.state.relationLimits = &limits
	q.state.relations = slices.Clone(q.state.relations)
	for i, p := range q.state.relations {
		q.state.relations[i] = relationWithLimits(p, &limits)
	}
	return q
}
func relationWithLimits(p *relationPlan, limits *RelationLimits) *relationPlan {
	if limits == nil {
		return p
	}
	p = copyRelationPlan(p)
	p.limits = limits
	return p
}
func rootRelationLimits(plans []*relationPlan) RelationLimits {
	if len(plans) > 0 && plans[0] != nil && plans[0].limits != nil {
		return *plans[0].limits
	}
	return DefaultRelationLimits()
}

// With adds batched eager loading. Example: Orders.Query().With(OrderRelations.Buyer).
func (q Query[M]) With(rels ...Relation[M]) Query[M] {
	q.state = withRelations(q.state, rels)
	return q
}
func withRelations[M any](q queryState, rels []Relation[M]) queryState {
	q.relations = slices.Clone(q.relations)
	for _, r := range rels {
		var p *relationPlan
		if !nilInterface(r) {
			p = r.relationPlan(nil)
		}
		q.relations = append(q.relations, relationWithLimits(p, q.relationLimits))
	}
	return q
}

func validateRelation(p *relationPlan, owner *tableCore) error {
	nodes := 0
	return validateRelationAt(p, owner, 1, DefaultRelationLimits(), &nodes)
}

// validateRelationMetadata checks deferred declarations without walking or
// compiling predicate subqueries. The outer compiler owns that recursive work
// and its shared resource budget.
func validateRelationMetadata(p *relationPlan, owner *tableCore) error {
	if p == nil {
		return invalid("nil relation descriptor")
	}
	if p.err != nil {
		return p.err
	}
	if p.aggregate != nil {
		return validateAggregateMetadata(p, owner)
	}
	if p.slot == nil || p.load == nil || p.loaded == nil {
		return invalid("relation requires a bound slot and loader")
	}
	if owner != nil && owner != p.slot.owner {
		return invalid("relation belongs to a different table declaration")
	}
	if p.external {
		return tableError(p.slot.owner)
	}
	if err := safeerror.Join("operation failed", tableError(p.slot.owner), tableError(p.target), p.query.err); err != nil {
		return err
	}
	if p.query.ast.Limit.IsSet() || p.query.ast.Offset != 0 {
		return invalid("relation scopes cannot use global limits or offsets")
	}
	if err := safeerror.Join("operation failed", p.query.ast.ValidateSoftDeleteScope(), p.query.ast.ValidateScopeOptions(), p.target.model.ValidateGlobalScopes()); err != nil {
		return err
	}
	return validateJoinedMetadata(p)
}
func validateRelationAt(p *relationPlan, owner *tableCore, depth int, limits RelationLimits, nodes *int) error {
	(*nodes)++
	if depth > limits.MaxDepth {
		return invalid("relation nesting exceeds its depth bound")
	}
	if *nodes > querycore.MaxExpressionNodes {
		return invalid("relation graph exceeds its resource bound")
	}
	if err := validateRelationMetadata(p, owner); err != nil {
		return err
	}
	if p.external {
		return nil
	}
	if p.aggregate != nil {
		if err := validateRelationAt(p.aggregate.input.plan, p.slot.owner, depth, limits, nodes); err != nil {
			return err
		}
		node, err := aggregateSelect(p.aggregate, nil, nil, 1)
		if err != nil {
			return err
		}
		compiler := querycore.Compiler{ScopeShapeOnly: true}
		compiler.BeginStatement(nil)
		_, err = compiler.CompileSelect(node)
		return err
	}
	if err := p.query.ast.Validate(); err != nil {
		return err
	}
	if _, err := p.query.ast.StableOrder(); err != nil {
		return err
	}
	if err := validateJoined(p, depth, limits, nodes); err != nil {
		return err
	}
	return validateRelationPlans(p.query.relations, p.target, depth, limits, nodes)
}
func validateRelationPlans(plans []*relationPlan, owner *tableCore, depth int, limits RelationLimits, nodes *int) error {
	if len(plans) > querycore.MaxExpressionNodes {
		return invalid("too many relation descriptors")
	}
	seen := make(map[string]bool, len(plans))
	for _, p := range plans {
		if err := validateRelationAt(p, owner, depth+1, limits, nodes); err != nil {
			return err
		}
		if owner == nil {
			owner = p.slot.owner
		}
		if seen[p.slot.name] {
			return invalid("relation slot was registered more than once")
		}
		seen[p.slot.name] = true
	}
	return nil
}
func validateRelations(q queryState) error {
	if len(q.relations) == 0 && q.relationLimits == nil {
		return nil
	}
	limits := rootRelationLimits(q.relations)
	if q.relationLimits != nil {
		limits = *q.relationLimits
	}
	if err := querycore.RelationLimits(limits).Validate(); err != nil {
		return err
	}
	if len(q.relations) == 0 {
		return nil
	}
	nodes := 0
	return validateRelationPlans(q.relations, q.table, 0, limits, &nodes)
}
func loadPlans(ctx context.Context, ex database.Executor, base unsafe.Pointer, count int, plans []*relationPlan) error {
	if len(plans) == 0 {
		return nil
	}
	limits := rootRelationLimits(plans)
	budget := relationBudget{limits: limits, remaining: limits.MaxRows, attachments: limits.MaxRows}
	return loadPlansAt(ctx, ex, base, count, plans, &budget, 0, false)
}
func loadPlansAt(ctx context.Context, ex database.Executor, base unsafe.Pointer, count int, plans []*relationPlan, budget *relationBudget, depth int, missing bool) error {
	for _, p := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.load(ctx, ex, p, base, count, budget, depth+1, missing); err != nil {
			return err
		}
	}
	return nil
}

// LoadFrom loads the query's relations onto copies of existing parents using its
// relation limits. Parent filters and windows are validated but do not re-fetch
// or filter supplied models. Errors publish no partial result.
// Example: loaded, err := orm.LoadFrom(ctx, db, q.With(OrderRelations.Buyer), orders).
func LoadFrom[M any](ctx context.Context, ex database.Executor, q Query[M], parents []M) ([]M, error) {
	return loadExistingQuery(ctx, ex, q.state, parents, false)
}

// LoadMissingFrom uses the query's relation limits while preserving loaded slots,
// including loaded-empty slots. It performs no parent statement.
// Example: loaded, err := orm.LoadMissingFrom(ctx, db, q.With(OrderRelations.Buyer), orders).
func LoadMissingFrom[M any](ctx context.Context, ex database.Executor, q Query[M], parents []M) ([]M, error) {
	return loadExistingQuery(ctx, ex, q.state, parents, true)
}

func loadExistingQuery[M any](ctx context.Context, ex database.Executor, q queryState, parents []M, missing bool) ([]M, error) {
	if err := executionContext(ctx, ex); err != nil {
		return nil, err
	}
	q.ast.ScopeContext = ctx
	if err := validateQuery(q); err != nil {
		return nil, err
	}
	plans := q.relations
	result := slices.Clone(parents)
	limits := rootRelationLimits(plans)
	budget := relationBudget{limits: limits, remaining: limits.MaxRows, attachments: limits.MaxRows}
	if err := loadPlansAt(ctx, ex, unsafe.Pointer(unsafe.SliceData(result)), len(result), plans, &budget, 0, missing); err != nil {
		return nil, err
	}
	return result, nil
}
