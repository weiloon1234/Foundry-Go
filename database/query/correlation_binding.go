package query

import (
	"maps"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Ordinary and transaction correlation share visibility and shadowing rules.
type correlationBinding[S, O, I any] struct {
	source projectionSource[S]
	outer  queryScope[O]
	inner  queryScope[I]
}

func bindCorrelation[S, O, I any](outer ScopeSource[O], inner ProjectionSource[I]) correlationBinding[S, O, I] {
	var r correlationBinding[S, O, I]
	if nilDescriptor(outer) || nilDescriptor(inner) {
		r.source.err = fault.New(fault.Invalid, "correlation requires outer and inner scopes")
		return r
	}
	r.outer = outer.scopeSource()
	input := inner.projectionSource()
	r.inner = scopeOf(input)
	r.source = projectionSource[S]{node: input.node, err: input.err}
	if r.outer.err != nil {
		r.source.err = r.outer.err
	}
	if r.inner.err != nil {
		r.source.err = r.inner.err
	}
	for name := range r.inner.sources {
		if _, exists := r.outer.sources[name]; exists {
			r.source.err = fault.New(fault.Invalid, "correlation shadows an outer source; use distinct aliases")
		}
	}
	return r
}
func combinedCorrelationScope[S any](outer, inner scopeRequirement, err error) queryScope[S] {
	sources := maps.Clone(outer.sources)
	if sources == nil {
		sources = make(map[string][]Column)
	}
	maps.Copy(sources, inner.sources)
	return queryScope[S]{scopeRequirement: scopeRequirement{sources: sources, err: err}}
}
