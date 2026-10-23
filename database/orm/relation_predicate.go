package orm

import (
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
	node := querycore.DirectRelationExists(p.query.ast.ModelSelect(nil), p.local, p.foreign, p.slot.owner.model.Columns)
	node.RelationCheck = func() error { return validateRelation(p, nil) }
	result.expression = querycore.SubqueryPredicate{Query: node}
	return result
}
