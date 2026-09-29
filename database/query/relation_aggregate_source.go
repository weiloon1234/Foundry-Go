package query

import "github.com/weiloon1234/Foundry-Go/fault"

// AggregateSource retains both the source and measured model. A many-to-many
// descriptor measures targets by default; Pivot selects its typed pivot input.
type AggregateSource[M, N any] interface{ aggregateInput() aggregateInput[M, N] }

type aggregateInput[M, N any] struct {
	source                 Query[M]
	local                  fieldRef
	inputTable, inputAlias string
	group                  fieldRef
	singular               bool
	unique                 fieldRef
	validate               func(int, RelationLimits) error
	selectNode             func() selectNode
}

func directAggregateInput[M, N any](spec relationSpec[M, N], validate func(string, int, RelationLimits) error, singular bool) aggregateInput[M, N] {
	if spec.hop != nil {
		return hopAggregateInput(spec, validate, singular)
	}
	return aggregateInput[M, N]{source: spec.source, local: spec.local, inputTable: spec.target.table, inputAlias: spec.target.table, group: spec.foreign, singular: singular,
		validate: func(depth int, limits RelationLimits) error {
			if len(spec.target.relations) != 0 {
				return fault.New(fault.Invalid, "aggregate input cannot request eager-loaded children")
			}
			return validate(spec.source.table, depth, limits)
		},
		selectNode: func() selectNode {
			return selectNode{source: tableSource{table: spec.target.table, columns: spec.target.definition.columns}, predicates: spec.target.effectivePredicates()}
		},
	}
}
func (r OneRelation[M, N]) aggregateInput() aggregateInput[M, N] {
	validate := r.validateRelation
	if r.ofMany != singleTarget || r.spec.morph != nil {
		validate = func(string, int, RelationLimits) error {
			return fault.New(fault.Invalid, "aggregates over a one-of-many relationship are not supported; aggregate its HasMany form")
		}
	}
	return directAggregateInput(r.spec, validate, true)
}
func (r ManyRelation[M, N]) aggregateInput() aggregateInput[M, N] {
	return directAggregateInput(r.spec, r.validateRelation, false)
}

func (r ThroughRelation[M, N, P]) aggregateInput() aggregateInput[M, N] {
	return aggregateInput[M, N]{source: r.spec.source, local: r.spec.local, inputTable: r.spec.target.table, inputAlias: throughTargetAlias, group: fieldRef{throughPivotAlias, r.pivotLocal.column},
		validate: func(depth int, limits RelationLimits) error {
			if len(r.spec.target.relations) != 0 || len(r.pivot.relations) != 0 {
				return fault.New(fault.Invalid, "aggregate input cannot request eager-loaded targets or pivots")
			}
			return r.validateRelation(r.spec.source.table, depth, limits)
		},
		selectNode: func() selectNode { n := r.throughSelect(); n.selections = nil; n.orders = nil; return n },
		unique:     pivotPrimary(r),
	}
}
func pivotPrimary[M, N, P any](r ThroughRelation[M, N, P]) fieldRef {
	if r.pivot.definition == nil {
		return fieldRef{}
	}
	return fieldRef{throughPivotAlias, r.pivot.definition.primary}
}

type pivotAggregateSource[M, N, P any] struct{ relation ThroughRelation[M, N, P] }

// Pivot selects the concrete pivot as the aggregate input. Target and pivot
// filters already attached to the relationship remain applied to its join.
func (r ThroughRelation[M, N, P]) Pivot() AggregateSource[M, P] {
	return pivotAggregateSource[M, N, P]{r}
}
func (p pivotAggregateSource[M, N, P]) aggregateInput() aggregateInput[M, P] {
	i := p.relation.aggregateInput()
	return aggregateInput[M, P]{source: i.source, local: i.local, inputTable: p.relation.pivot.table, inputAlias: throughPivotAlias, group: i.group, unique: i.unique, validate: i.validate, selectNode: i.selectNode}
}
