package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Correlation separates an explicit outer/inner scope pair from either input.
type Correlation[Outer, Inner any] struct {
	_ [0]*Outer
	_ [0]*Inner
	_ [0]struct{ correlation bool }
}

// ScopeSource describes fields visible at a SELECT boundary. Model/alias/join
// sources and a containing correlation expose it; it performs no database work.
type ScopeSource[S any] interface{ scopeSource() queryScope[S] }
type queryScope[S any] struct {
	_ [0]*S
	scopeRequirement
}
type scopeRequirement struct {
	sources map[string][]Column
	err     error
}

func scopeOf[S any](source projectionSource[S]) queryScope[S] {
	r := queryScope[S]{scopeRequirement: scopeRequirement{sources: make(map[string][]Column), err: source.err}}
	add := func(table tableSource) {
		if _, exists := r.sources[table.name()]; exists {
			r.err = fault.New(fault.Invalid, "scope repeats a source name")
		}
		r.sources[table.name()] = table.columns
	}
	add(source.node.source)
	for _, join := range source.node.joins {
		add(join.source)
	}
	return r
}
func (q Query[M]) scopeSource() queryScope[M] { return scopeOf(q.projectionSource()) }
func (a AliasedSource[A, M]) scopeSource() queryScope[Alias[A, M]] {
	return scopeOf(a.projectionSource())
}
func (j joinedSource[S, L, R]) scopeSource() queryScope[S] { return scopeOf(j.projectionSource()) }

// Scope exposes the base model scope without repeating its table declaration.
func (q Query[M]) Scope() ModelScope[M, M] {
	if q.projectionSource().err != nil {
		return ModelScope[M, M]{}
	}
	return ModelScope[M, M]{table: q.table, record: q.recordQuery().metadata()}
}

// CorrelatedSource makes outer fields explicitly available inside one inner
// SELECT. It deliberately has no standalone execution or ordinary source methods.
// SelectCorrelatedValue and Exists keep the required outer scope in their types.
type CorrelatedSource[O, I any] struct {
	source projectionSource[Correlation[O, I]]
	outer  queryScope[O]
	inner  queryScope[I]
}

// Correlate binds an inner query to the outer row. Filters on outer still belong
// to the outer SELECT. Use distinct SQL aliases where names would shadow an
// outer source, including self-correlation. A containing correlation can be outer.
func Correlate[O, I any](outer ScopeSource[O], inner ProjectionSource[I]) CorrelatedSource[O, I] {
	state := bindCorrelation[Correlation[O, I]](outer, inner)
	return CorrelatedSource[O, I]{source: state.source, outer: state.outer, inner: state.inner}
}
func (s CorrelatedSource[O, I]) scopeSource() queryScope[Correlation[O, I]] {
	return combinedCorrelationScope[Correlation[O, I]](s.outer.scopeRequirement, s.inner.scopeRequirement, s.source.err)
}
func (s CorrelatedSource[O, I]) Where(predicates ...Predicate[Correlation[O, I]]) CorrelatedSource[O, I] {
	s.source = s.source.where(predicates...)
	return s
}
func (s CorrelatedSource[O, I]) Limit(count int) CorrelatedSource[O, I] {
	s.source.node.limit = value.Set(count)
	return s
}
func (s CorrelatedSource[O, I]) Offset(count int) CorrelatedSource[O, I] {
	s.source.node.offset = count
	return s
}

// Exists yields a predicate owned by the required outer scope, not the inner one.
func (s CorrelatedSource[O, I]) Exists() Predicate[O] {
	return Predicate[O]{expression: subqueryPredicate{query: subquery{node: s.source.node, err: s.source.err, correlation: &s.outer.scopeRequirement}}}
}

// Private source adaptation reuses projection execution without exposing an
// erasure path from a correlated source to an ordinary executable query.
func (s projectionSource[S]) projectionSource() projectionSource[S] { return s }
