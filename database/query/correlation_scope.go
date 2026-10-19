package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

type scopeRequirement = querycore.ScopeRequirement

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
	_                [0]*S
	scopeRequirement scopeRequirement
}

func scopeOf[S any](source projectionSource[S]) queryScope[S] {
	r := queryScope[S]{scopeRequirement: scopeRequirement{Sources: make(map[string][]columnData), Err: source.err}}
	add := func(table tableSource) {
		if _, exists := r.scopeRequirement.Sources[table.Name()]; exists {
			r.scopeRequirement.Err = fault.New(fault.Invalid, "scope repeats a source name")
		}
		r.scopeRequirement.Sources[table.Name()] = table.Columns
	}
	add(source.node.Source)
	for _, join := range source.node.Joins {
		add(join.Source)
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
	return ModelScope[M, M]{table: q.queryCore.Table, record: q.recordQuery().metadata()}
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
	s.source.node.Limit = value.Set(count)
	return s
}

func (s CorrelatedSource[O, I]) Offset(count int) CorrelatedSource[O, I] {
	s.source.node.Offset = count
	return s
}

// Exists yields a predicate owned by the required outer scope, not the inner one.
func (s CorrelatedSource[O, I]) Exists() Predicate[O] {
	return Predicate[O]{expression: subqueryPredicate{Query: subquery{Node: s.source.node, Err: s.source.err, Correlation: &s.outer.scopeRequirement}}}
}

// Private source adaptation reuses projection execution without exposing an
// erasure path from a correlated source to an ordinary executable query.
func (s projectionSource[S]) projectionSource() projectionSource[S] { return s }
