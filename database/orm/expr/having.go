package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Not negates a group predicate. Example: count.Gt(0).Not().
func (p HavingPredicate[S]) Not() HavingPredicate[S] {
	p.core.Node = querycore.Negation{Child: p.core.Node}
	return p
}

// HavingAnd joins group predicates with AND; empty is TRUE. Example: expr.HavingAnd(count.Gt(0),sum.IsNotNull()).
func HavingAnd[S any](predicates ...HavingPredicate[S]) HavingPredicate[S] {
	return combineHaving(false, predicates)
}

// HavingOr joins group predicates with OR; empty is FALSE. Example: expr.HavingOr(count.Gt(0),sum.IsNull()).
func HavingOr[S any](predicates ...HavingPredicate[S]) HavingPredicate[S] {
	return combineHaving(true, predicates)
}
func combineHaving[S any](any bool, predicates []HavingPredicate[S]) HavingPredicate[S] {
	if len(predicates) > querycore.MaxExpressionNodes {
		return HavingPredicate[S]{core: ormcore.Predicate{Err: ormcore.Invalid("conjunction exceeds its resource bound")}}
	}
	ps := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		ps[i] = p.core
	}
	out := ormcore.Conjunction(ps)
	if node, ok := out.Node.(querycore.Junction); ok {
		node.Any = any
		out.Node = node
	}
	return HavingPredicate[S]{core: out}
}

// FoundryHaving supplies validated group-condition metadata to rows. Example: FoundryHaving(p,sqlowner.Expressions()).
func FoundryHaving[S any](p HavingPredicate[S], seal sqlowner.ExpressionSeal) ormcore.Predicate {
	if !seal.Valid() {
		return ormcore.Predicate{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return p.core
}
