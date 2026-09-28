package http

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// MergeQueries combines descriptors for the same concrete query type. It
// reuses their field bindings and metadata, rejects duplicate wire names, and
// preserves the ordinary strict query parser. No precedence is assigned to an
// overlapping parameter. An empty list defines a query with no parameters.
func MergeQueries[Q any](queries ...Query[Q]) Query[Q] {
	var parameters []QueryParameter[Q]
	for _, query := range queries {
		if err := query.Validate(); err != nil {
			return Query[Q]{err: err}
		}
		parameters = append(parameters, query.parameters...)
	}
	return DefineQuery(parameters...)
}

// EmbedQuery binds an existing query descriptor to a concrete field of an outer
// query value. Wire names, required/repeated behavior, defaults and scalar
// metadata remain owned by the original bindings; embedding adds no wire prefix.
// The selector must select a field of its argument, never retain it, and be safe
// for concurrent calls on independent arguments. It executes inside the same
// owned callback boundary as ordinary query field selectors.
func EmbedQuery[Outer, Inner any](query Query[Inner], field func(*Outer) *Inner) Query[Outer] {
	if err := query.Validate(); err != nil {
		return Query[Outer]{err: err}
	}
	if field == nil {
		return Query[Outer]{err: fault.New(fault.Invalid, "query embedding requires a field selector")}
	}
	parameters := make([]QueryParameter[Outer], 0, len(query.parameters))
	for _, parameter := range query.parameters {
		parameters = append(parameters, QueryParameter[Outer]{info: parameter.info, scalar: parameter.scalar,
			decode: func(ctx context.Context, outer *Outer, values []string) *queryBindingFailure {
				inner := field(outer)
				if inner == nil {
					return querySelectorFailure()
				}
				return parameter.decode(ctx, inner, values)
			},
			encode: func(ctx context.Context, outer *Outer, budget *queryBudget) ([]string, *queryBindingFailure) {
				inner := field(outer)
				if inner == nil {
					return nil, querySelectorFailure()
				}
				return parameter.encode(ctx, inner, budget)
			},
		})
	}
	return DefineQuery(parameters...)
}
