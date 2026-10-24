package orm

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"slices"
	"unsafe"
)

type relationJoin struct {
	defaults      []querycore.MutationAssignment
	pivot         queryState
	first, second querycore.FieldRef
	hop           bool
	orders        []querycore.RelationOrder
}
type relationMorph struct {
	kind *columnCore
	name string
}

func (m *relationMorph) matches(base unsafe.Pointer) (bool, error) {
	if m == nil {
		return true, nil
	}
	v, err := m.kind.get(base)
	s, ok := v.(string)
	return err == nil && ok && s == m.name, err
}
func copyJoined(p *relationPlan) *relationPlan {
	p = copyRelationPlan(p)
	if p.join == nil {
		p.err = errors.Join(p.err, invalid("relation requires a joined plan"))
		return p
	}
	j := *p.join
	p.join = &j
	return p
}
func relationTargetState(p *relationPlan, ctx context.Context) (queryState, error) {
	q := p.query
	q.ast.ScopeContext = ctx
	if p.choice != 0 {
		if p.choice > 1 {
			q.ast.Orders = append(slices.Clone(q.ast.Orders), querycore.QueryOrder{Field: querycore.FieldRef{Table: q.ast.Table, Column: q.table.model.Primary}, Descending: p.choice == 2})
		}
		key := p.foreign
		if p.join != nil && p.join.hop {
			key = querycore.FieldRef{Table: querycore.RelationHopAlias, Column: p.join.first.Column}
		}
		q.ast.DistinctOnKey = &key
	}
	var err error
	q.ast, err = q.ast.StableOrder()
	return q, err
}
func relationSelect(p *relationPlan, ctx context.Context) (querycore.SelectNode, error) {
	if p.join != nil && !p.join.hop {
		a, b := p.query.ast, p.join.pivot.ast
		a.ScopeContext = ctx
		b.ScopeContext = ctx
		return querycore.RelationThroughSelect(a, b, p.foreign, p.join.second, p.join.orders, querycore.RelationTargetAlias, querycore.RelationPivotAlias), nil
	}
	q, err := relationTargetState(p, ctx)
	if err != nil {
		return querycore.SelectNode{}, err
	}
	node := q.ast.ModelSelect(p.target.selections)
	if p.join != nil {
		j := p.join
		hop := j.pivot.ast
		hop.ScopeContext = ctx
		node = querycore.RelationHopJoin(node, q.ast.Table, p.foreign, hop.Model.Source(hop.Table, ""), hop.EffectivePredicates(), j.second)
	}
	return node, nil
}
func validateJoinedMetadata(p *relationPlan) error {
	if p.join == nil {
		return nil
	}
	q := p.join.pivot
	if err := errors.Join(tableError(q.table), q.err); err != nil {
		return err
	}
	if q.ast.Limit.IsSet() || q.ast.Offset != 0 {
		return invalid("pivot scopes cannot use global windows")
	}
	return errors.Join(q.ast.ValidateSoftDeleteScope(), q.ast.ValidateScopeOptions(), q.table.model.ValidateGlobalScopes())
}
func validateJoined(p *relationPlan, depth int, limits RelationLimits, nodes *int) error {
	if p.choice > 3 || p.choice == 1 && len(p.query.ast.Orders) == 0 {
		return invalid("one-of-many requires at least one order")
	}
	if p.join == nil {
		if p.choice != 0 {
			_, err := relationTargetState(p, nil)
			return err
		}
		return nil
	}
	j := p.join
	if err := j.pivot.ast.Validate(); err != nil {
		return err
	}
	if err := validateRelationPlans(j.pivot.relations, j.pivot.table, depth, limits, nodes); err != nil {
		return err
	}
	if len(j.orders) > querycore.MaxExpressionNodes {
		return invalid("relation ordering exceeds its resource bound")
	}
	seen := map[querycore.FieldRef]bool{}
	orderNodes := 0
	for _, o := range j.orders {
		table := p.query.ast.Table
		if o.Pivot {
			table = j.pivot.ast.Table
		}
		if err := querycore.ValidateRowValue(o.Value, func(f querycore.FieldRef) error { return f.Validate(table) }, 0, &orderNodes); err != nil {
			return err
		}
		alias := querycore.RelationTargetAlias
		if o.Pivot {
			alias = querycore.RelationPivotAlias
		}
		f, ok := o.Value.(querycore.FieldRef)
		if !ok {
			continue
		}
		f.Table = alias
		if seen[f] {
			return invalid("repeated relation ordering column")
		}
		seen[f] = true
	}
	node, err := relationSelect(p, nil)
	if err != nil {
		return err
	}
	c := querycore.Compiler{ScopeShapeOnly: true}
	_, err = c.CompileSelect(node)
	return err
}
func relationVisibility(p *relationPlan, scope querycore.SoftDeleteScope, pivot bool) *relationPlan {
	p = copyRelationPlan(p)
	if pivot {
		p = copyJoined(p)
		if p.join != nil {
			p.join.pivot.ast.SoftDeleteScope = scope
		}
	} else {
		p.query.ast.SoftDeleteScope = scope
	}
	return p
}
func ofMany(p *relationPlan, choice uint8) *relationPlan {
	p = copyRelationPlan(p)
	p.choice = choice
	return p
}
func relationThroughOrder[N any](p *relationPlan, orders []Order[N], pivot bool) *relationPlan {
	p = copyJoined(p)
	if p.join == nil {
		return p
	}
	p.join.orders = slices.Clone(p.join.orders)
	for _, o := range orders {
		owner := p.target
		if pivot {
			owner = p.join.pivot.table
		}
		p.err = errors.Join(p.err, o.err)
		if o.owner != owner {
			p.err = errors.Join(p.err, invalid("order belongs to a different table declaration"))
		}
		p.join.orders = append(p.join.orders, querycore.RelationOrder{Value: o.core.Value(), Descending: o.core.Descending, Nulls: o.core.Nulls, Pivot: pivot})
	}
	return p
}
func relationPivotWhere[P any](p *relationPlan, ps []Predicate[P]) *relationPlan {
	p = copyJoined(p)
	if p.join != nil {
		p.join.pivot = where(p.join.pivot, ps)
	}
	return p
}
func relationPivotWith[P any](p *relationPlan, rels []Relation[P]) *relationPlan {
	p = copyJoined(p)
	if p.join != nil {
		p.join.pivot = withRelations(p.join.pivot, rels)
	}
	return p
}
