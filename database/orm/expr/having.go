package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// HavingNot negates a group predicate without changing its phase or scope.
//
// Example: expr.HavingNot(p)
func HavingNot[S any](p HavingPredicate[S]) HavingPredicate[S] {
	p.core.Node = querycore.Negation{Child: p.core.Node}
	return p
}

// HavingAnd joins group predicates with AND; empty is TRUE.
//
// Example: expr.HavingAnd(predicates...)
func HavingAnd[S any](predicates ...HavingPredicate[S]) HavingPredicate[S] {
	return combineHaving(false, predicates)
}

// HavingOr joins group predicates with OR; empty is FALSE.
//
// Example: expr.HavingOr(predicates...)
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

// FoundryHaving supplies validated group-condition metadata to rows.
//
// Example: expr.FoundryHaving(p, seal)
func FoundryHaving[S any](p HavingPredicate[S], seal sqlowner.ExpressionSeal) ormcore.Predicate {
	if !seal.Valid() {
		return ormcore.Predicate{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return p.core
}
