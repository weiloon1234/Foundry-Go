package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionCorrelation is the explicit outer/inner owner of a Tx-required SELECT.
type TransactionCorrelation[O, I TransactionScope] struct {
	_ [0]*O
	_ [0]*I
	_ [0]struct{ transactionCorrelation bool }
}

func (TransactionCorrelation[O, I]) transactionScope() {}

// TransactionScopeSource describes fields visible to a Tx-required correlation.
type TransactionScopeSource[S TransactionScope] interface{ transactionScopeSource() queryScope[S] }
type transactionScopeAdapter[S TransactionScope] struct{ scope queryScope[S] }

func (s transactionScopeAdapter[S]) scopeSource() queryScope[S] { return s.scope }
func transactionScopeOf[S TransactionScope](source TransactionScopeSource[S]) transactionScopeAdapter[S] {
	if nilDescriptor(source) {
		return transactionScopeAdapter[S]{scope: queryScope[S]{scopeRequirement: scopeRequirement{err: fault.New(fault.Invalid, "transaction correlation requires an outer scope")}}}
	}
	return transactionScopeAdapter[S]{scope: source.transactionScopeSource()}
}
func (a TransactionAliasedSource[A, R]) transactionScopeSource() queryScope[TransactionAlias[A, R]] {
	return scopeOf(a.transactionProjection())
}
func (j transactionJoinedSource[S, L, R]) transactionScopeSource() queryScope[S] {
	return scopeOf(j.transactionProjection())
}

// TransactionCorrelatedSource preserves its declared parent and cannot execute
// independently or be passed as an uncorrelated transaction projection source.
type TransactionCorrelatedSource[O, I TransactionScope] struct {
	source projectionSource[TransactionCorrelation[O, I]]
	outer  queryScope[O]
	inner  queryScope[I]
}

// TransactionCorrelate makes the declared outer fields available to one inner
// SELECT. Outer filters still belong to the outer query; aliases must not shadow.
func TransactionCorrelate[O, I TransactionScope](outer TransactionScopeSource[O], inner TransactionProjectionSource[I]) TransactionCorrelatedSource[O, I] {
	state := bindCorrelation[TransactionCorrelation[O, I]](transactionScopeOf(outer), transactionProjection(inner))
	return TransactionCorrelatedSource[O, I]{source: state.source, outer: state.outer, inner: state.inner}
}
func (s TransactionCorrelatedSource[O, I]) transactionScopeSource() queryScope[TransactionCorrelation[O, I]] {
	return combinedCorrelationScope[TransactionCorrelation[O, I]](s.outer.scopeRequirement, s.inner.scopeRequirement, s.source.err)
}
func (s TransactionCorrelatedSource[O, I]) outerCorrelation() correlationBoundary[TransactionCorrelation[O, I], O] {
	return correlationBoundaryFor[TransactionCorrelation[O, I], O](s.outer.scopeRequirement, s.source.err)
}
func (s TransactionCorrelatedSource[O, I]) innerCorrelation() correlationBoundary[TransactionCorrelation[O, I], I] {
	return correlationBoundaryFor[TransactionCorrelation[O, I], I](s.inner.scopeRequirement, s.source.err)
}
func (s TransactionCorrelatedSource[O, I]) Where(predicates ...Predicate[TransactionCorrelation[O, I]]) TransactionCorrelatedSource[O, I] {
	s.source = s.source.where(predicates...)
	return s
}
func (s TransactionCorrelatedSource[O, I]) Limit(count int) TransactionCorrelatedSource[O, I] {
	s.source.node.limit = value.Set(count)
	return s
}
func (s TransactionCorrelatedSource[O, I]) Offset(count int) TransactionCorrelatedSource[O, I] {
	s.source.node.offset = count
	return s
}

// Exists retains the parent requirement and any locks in the inner sources.
func (s TransactionCorrelatedSource[O, I]) Exists() Predicate[O] {
	return Predicate[O]{expression: subqueryPredicate{query: subquery{node: s.source.node, err: s.source.err, correlation: &s.outer.scopeRequirement}}}
}
