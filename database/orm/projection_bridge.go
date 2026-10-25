package orm

import (
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// FoundryStatement retains the shared immutable inspection boundary. Example: FoundryStatement(s,sqlowner.Metadata()).
func FoundryStatement(s querycore.Statement, seal sqlowner.MetadataSeal) (Statement, error) {
	if !seal.Valid() {
		return Statement{}, invalid("invalid SQL owner capability")
	}
	return Statement{core: querycore.RetainStatement(s)}, nil
}

// FoundryPredicate retains model declaration ownership in a projection. Example: FoundryPredicate(p,sqlowner.Metadata()).
func FoundryPredicate[M any](p Predicate[M], seal sqlowner.MetadataSeal) ormcore.Predicate {
	if !seal.Valid() {
		return ormcore.Predicate{Err: invalid("invalid SQL owner capability")}
	}
	result := ormcore.Predicate{Node: querycore.PredicateNode(p.expression), Err: p.err}
	if p.owner != nil {
		result.Err = errors.Join(result.Err, declarationCoreError(p.owner))
		result.Refs = []ormcore.Reference{{Scope: reflect.TypeFor[M](), Table: p.owner.model.Table, Owner: p.owner}}
	} else if p.rawScope == reflect.TypeFor[M]() {
		result.Refs = []ormcore.Reference{{Scope: p.rawScope}}
	} else {
		result.Err = errors.Join(result.Err, invalid("predicate requires a model declaration"))
	}
	return result
}

// FoundryRelationPredicate validates projected child conditions at the relation owner. Example: FoundryRelationPredicate(r,ps,sqlowner.Metadata()).
func FoundryRelationPredicate[M, N any](r ExistenceRelation[M, N], ps []ormcore.Predicate, seal sqlowner.MetadataSeal) Predicate[M] {
	if !seal.Valid() {
		return Predicate[M]{err: invalid("invalid SQL owner capability")}
	}
	if nilInterface(r) {
		return Predicate[M]{err: invalid("nil relation descriptor")}
	}
	plan, err := relationPredicatePlan(r.relationPlan(nil), reflect.TypeFor[M](), reflect.TypeFor[N]())
	if err != nil {
		return Predicate[M]{err: err}
	}

	predicates := make([]Predicate[N], len(ps))
	for i, p := range ps {
		predicates[i] = relationExpressionPredicate[N](plan, p)
	}
	return relationExistence[M](relationWhere(plan, predicates))
}

// FoundryRelationFilter validates expression conditions for a relation target. Example: FoundryRelationFilter(r,p,sqlowner.Metadata()).
func FoundryRelationFilter[M, N any](r ExistenceRelation[M, N], p ormcore.Predicate, seal sqlowner.MetadataSeal) Predicate[N] {
	if !seal.Valid() {
		return Predicate[N]{err: invalid("invalid SQL owner capability")}
	}
	if nilInterface(r) {
		return Predicate[N]{err: invalid("nil relation descriptor")}
	}
	plan, err := relationPredicatePlan(r.relationPlan(nil), reflect.TypeFor[M](), reflect.TypeFor[N]())
	if err != nil {
		return Predicate[N]{err: err}
	}
	return relationExpressionPredicate[N](plan, p)
}
