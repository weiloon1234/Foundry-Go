package orm

import (
	"context"
	"errors"
	"slices"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// RelationLimits is the existing shared public limits type. MaxRows separately
// bounds fetched rows and attached values across the whole tree.
// Example: limits := orm.DefaultRelationLimits().
type RelationLimits = query.RelationLimits

// DefaultRelationLimits uses the shared defaults: batches 500, rows 10000, depth 8.
// Example: limits := orm.DefaultRelationLimits().
func DefaultRelationLimits() RelationLimits { return query.DefaultRelationLimits() }

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
func validateRelationAt(p *relationPlan, owner *tableCore, depth int, limits RelationLimits, nodes *int) error {
	(*nodes)++
	if depth > limits.MaxDepth {
		return invalid("relation nesting exceeds its depth bound")
	}
	if *nodes > querycore.MaxExpressionNodes {
		return invalid("relation graph exceeds its resource bound")
	}
	if p == nil {
		return invalid("nil relation descriptor")
	}
	if p.err != nil {
		return p.err
	}
	if p.slot == nil || p.load == nil || p.loaded == nil {
		return invalid("relation requires a bound slot and loader")
	}
	if owner != nil && owner != p.slot.owner {
		return invalid("relation belongs to a different table declaration")
	}
	if err := errors.Join(tableError(p.slot.owner), tableError(p.target), p.query.err); err != nil {
		return err
	}
	if p.query.ast.Limit.IsSet() || p.query.ast.Offset != 0 {
		return invalid("relation scopes cannot use global limits or offsets")
	}
	if err := errors.Join(p.query.ast.Validate(), p.target.model.ValidateGlobalScopes()); err != nil {
		return err
	}
	if _, err := p.query.ast.StableOrder(); err != nil {
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

// Load returns copies with loaded slots, without fetching parents. Errors discard
// partial results. Supplied parent provenance and authorization belong to callers.
// Example: loaded, err := orm.Load(ctx,db,orders,OrderRelations.Buyer).
func Load[M any](ctx context.Context, ex database.Executor, parents []M, rels ...Relation[M]) ([]M, error) {
	return loadExisting(ctx, ex, parents, false, rels)
}

// LoadMissing preserves already-loaded slots, including loaded-empty slots, and
// performs no parent query. Example: orm.LoadMissing(ctx,db,orders,OrderRelations.Buyer).
func LoadMissing[M any](ctx context.Context, ex database.Executor, parents []M, rels ...Relation[M]) ([]M, error) {
	return loadExisting(ctx, ex, parents, true, rels)
}
func loadExisting[M any](ctx context.Context, ex database.Executor, parents []M, missing bool, rels []Relation[M]) ([]M, error) {
	if err := executionContext(ctx, ex); err != nil {
		return nil, err
	}
	q := withRelations(queryState{}, rels)
	if err := validateRelations(q); err != nil {
		return nil, err
	}
	result := slices.Clone(parents)
	limits := rootRelationLimits(q.relations)
	budget := relationBudget{limits: limits, remaining: limits.MaxRows, attachments: limits.MaxRows}
	if err := loadPlansAt(ctx, ex, unsafe.Pointer(unsafe.SliceData(result)), len(result), q.relations, &budget, 0, missing); err != nil {
		return nil, err
	}
	return result, nil
}
