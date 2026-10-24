package orm

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// ExistenceRelation retains both source and target owners for predicate inference.
// Example: orm.Has(UserRelations.Orders, Orders.TotalCents.Gt(100)).
type ExistenceRelation[M, N any] interface {
	Relation[M]
	relationTarget(*N)
}

// Has matches parents with related rows satisfying the target predicates and
// related scopes, without eager loading. Example: Users.Query().Where(orm.Has(UserRelations.Orders)).
func Has[M, N any](r ExistenceRelation[M, N], ps ...Predicate[N]) Predicate[M] {
	if nilInterface(r) {
		return Predicate[M]{err: invalid("nil relation descriptor")}
	}
	p := relationWhere(r.relationPlan(nil), ps)
	return relationExistence[M](p)
}

// DoesntHave matches parents with no scoped related match, without extra SQL.
// Example: Users.Query().Where(orm.DoesntHave(UserRelations.Orders)).
func DoesntHave[M, N any](r ExistenceRelation[M, N], ps ...Predicate[N]) Predicate[M] {
	return Not(Has(r, ps...))
}

func relationExistence[M any](p *relationPlan) Predicate[M] {
	p = copyRelationPlan(p)
	p.query.ast.Orders = nil
	p.query.relations = nil
	p.choice = 0
	if p.join != nil {
		p = copyJoined(p)
		p.join.orders = nil
		p.join.pivot.relations = nil
	}
	result := Predicate[M]{}
	if p.slot != nil {
		result.owner = p.slot.owner
	}
	if p.err != nil {
		result.err = p.err
		return result
	}
	if p.slot == nil || p.slot.owner == nil || p.target == nil {
		result.err = invalid("relation requires bound source and target metadata")
		return result
	}
	if err := errors.Join(declarationCoreError(p.slot.owner), declarationCoreError(p.target), p.query.err); err != nil {
		result.err = err
		return result
	}
	node := relationExistsNode(p)
	node.RelationCheck = func() error { return validateRelationMetadata(p, nil) }
	result.expression = querycore.SubqueryPredicate{Query: node}
	return result
}

func relationExistsNode(p *relationPlan) querycore.Subquery {
	columns := p.slot.owner.model.Columns
	if p.join == nil {
		node := querycore.DirectRelationExists(p.query.ast.ModelSelect(nil), p.local, p.foreign, columns)
		if p.morph != nil {
			c := p.morph.kind
			node.Node.Predicates = append(node.Node.Predicates, querycore.Comparison{Operand: querycore.FieldRef{Table: p.slot.owner.model.Table, Column: c.metadata.Name}, Operator: querycore.Equal, Values: []any{p.morph.name}, Bind: func(v any) (driver.Value, error) { return v, nil }})
		}
		return node
	}
	j := p.join
	if j.hop {
		node, alias, err := querycore.RelationExistsSelect(p.query.ast.ModelSelect(nil), p.local.Table)
		if err != nil {
			return querycore.Subquery{Err: err}
		}
		node = querycore.RelationHopJoin(node, alias, p.foreign, j.pivot.table.model.Source(j.pivot.ast.Table, ""), j.pivot.ast.EffectivePredicates(), j.second)
		node.Predicates = append(node.Predicates, querycore.BinaryComparison{Left: querycore.FieldRef{Table: querycore.RelationHopAlias, Column: j.first.Column}, Right: p.local, Operator: querycore.Equal})
		return querycore.RelationExistence(node, p.local.Table, columns)
	}
	names, err := querycore.NamesInSelects(p.query.ast.ModelSelect(nil), j.pivot.ast.ModelSelect(nil))
	if err != nil {
		return querycore.Subquery{Err: err}
	}
	names.Used[p.local.Table] = true
	target, pivot := names.Allocate("foundry_related"), names.Allocate("foundry_link")
	node := querycore.RelationThroughSelect(p.query.ast, j.pivot.ast, p.foreign, j.second, nil, target, pivot)
	node.Selections = nil
	node.Orders = nil
	node.Predicates = append(node.Predicates, querycore.BinaryComparison{Left: querycore.FieldRef{Table: pivot, Column: j.first.Column}, Right: p.local, Operator: querycore.Equal})
	return querycore.RelationExistence(node, p.local.Table, columns)
}
