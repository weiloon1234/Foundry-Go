package query

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ProjectionSource retains the input scope of a query separately from its result.
// Generated model queries implement it through their embedded Query value.
type ProjectionSource[S any] interface{ projectionSource() projectionSource[S] }
type projectionSource[S any] struct {
	_    [0]*S
	node selectNode
	err  error
}

func (q Query[M]) projectionSource() projectionSource[M] {
	s := projectionSource[M]{}
	if q.definition == nil {
		s.err = fault.New(fault.Invalid, "projection source requires model metadata")
		return s
	}
	if err := q.validateSoftDeleteScope(); err != nil {
		s.err = err
		return s
	}
	s.err = q.definition.Validate()
	if len(q.relations) != 0 || q.relationLimits != nil {
		s.err = fault.New(fault.Invalid, "projection source cannot request eager-loaded model values")
	}
	s.node = q.modelSelect()
	s.node.selections = nil
	return s
}

// ProjectionQuery preserves input scope S through filtering/grouping and returns
// the complete declared record P. It has no model mutation or key-lookup methods.
type ProjectionQuery[S, P any] struct {
	source     projectionSource[S]
	definition ProjectionDefinition[P]
	mappings   []ProjectionMapping[S, P]
	record     *recordSelection[P]
}

// Project is the generated selection boundary. Every result field must have
// exactly one type-compatible expression; mapping order never changes decoding.
func Project[S, P any](source ProjectionSource[S], definition ProjectionDefinition[P], mappings ...ProjectionMapping[S, P]) ProjectionQuery[S, P] {
	q := ProjectionQuery[S, P]{definition: definition, mappings: slices.Clone(mappings)}
	if nilDescriptor(source) {
		q.source.err = fault.New(fault.Invalid, "projection requires a source")
		return q
	}
	q.source = source.projectionSource()
	return q
}
func (q ProjectionQuery[S, P]) Where(predicates ...Predicate[S]) ProjectionQuery[S, P] {
	q.source = q.source.where(predicates...)
	return q
}
func (s projectionSource[S]) where(predicates ...Predicate[S]) projectionSource[S] {
	s.node.predicates = slices.Clone(s.node.predicates)
	for _, p := range predicates {
		s.node.predicates = append(s.node.predicates, p.expression)
	}
	return s
}
func (q ProjectionQuery[S, P]) OrderBy(orders ...ProjectionOrder[S]) ProjectionQuery[S, P] {
	q.source.node.orders = slices.Clone(q.source.node.orders)
	for _, o := range orders {
		if nilDescriptor(o) {
			q.source.err = fault.New(fault.Invalid, "projection ordering requires an expression")
			continue
		}
		q.source.node.orders = append(q.source.node.orders, o.projectionOrder().node)
	}
	return q
}

// Having appends group filters, independently of the source's row-level Where.
func (q ProjectionQuery[S, P]) Having(predicates ...HavingPredicate[S]) ProjectionQuery[S, P] {
	q.source.node.having = slices.Clone(q.source.node.having)
	for _, p := range predicates {
		q.source.node.having = append(q.source.node.having, p.expression)
	}
	return q
}
func (q ProjectionQuery[S, P]) GroupBy(groups ...Group[S]) ProjectionQuery[S, P] {
	q.source.node.groupBy = append(slices.Clone(q.source.node.groupBy), keyExpressions(groups)...)
	return q
}
func (q ProjectionQuery[S, P]) Limit(count int) ProjectionQuery[S, P] {
	q.source.node.limit = value.Set(count)
	return q
}
func (q ProjectionQuery[S, P]) Offset(count int) ProjectionQuery[S, P] {
	q.source.node.offset = count
	return q
}

func (q ProjectionQuery[S, P]) selectNode() (selectNode, error) {
	if q.source.err != nil {
		return selectNode{}, q.source.err
	}
	if q.record != nil {
		return q.record.selectNode(q.source.node)
	}
	if err := q.definition.Validate(); err != nil {
		return selectNode{}, err
	}
	if len(q.mappings) != len(q.definition.columns) {
		return selectNode{}, fault.New(fault.Invalid, "projection must select every declared result field exactly once")
	}
	byName := make(map[string]ProjectionMapping[S, P], len(q.mappings))
	for _, m := range q.mappings {
		if _, exists := byName[m.column.name]; exists {
			return selectNode{}, fault.New(fault.Invalid, "repeated projection field mapping")
		}
		byName[m.column.name] = m
	}
	node := q.source.node
	node.selections = make([]selectItem, len(q.definition.columns))
	for i, column := range q.definition.columns {
		mapping, exists := byName[column.name]
		if !exists || mapping.column.typ != column.typ {
			return selectNode{}, fault.New(fault.Invalid, "projection mapping does not match its declared field")
		}
		node.selections[i] = selectItem{expression: mapping.expression, alias: column.name}
	}
	return node, nil
}
func (q ProjectionQuery[S, P]) reader() readResult[P] {
	node, err := q.selectNode()
	metadata := q.recordMetadata()
	return readResult[P]{node: node, scan: metadata.scan, lifecycle: metadata.lifecycle, err: err}
}

func (q ProjectionQuery[S, P]) recordMetadata() recordMetadata[P] {
	if q.record != nil && q.record.metadata != nil {
		return *q.record.metadata
	}
	return recordMetadata[P]{columns: q.definition.sourceColumns(), scan: q.definition.scan, fields: q.definition.fields}
}

// Compile validates and compiles the query without executing it.
func (q ProjectionQuery[S, P]) Compile() (Statement, error) { return q.reader().Compile() }

// Each streams complete results and closes rows when its callback returns or fails.
func (q ProjectionQuery[S, P]) Each(ctx context.Context, executor database.Executor, yield func(P) error) error {
	return q.reader().Each(ctx, executor, yield)
}

// All collects results, discarding partial output on failure.
func (q ProjectionQuery[S, P]) All(ctx context.Context, executor database.Executor) ([]P, error) {
	return q.reader().All(ctx, executor)
}

// First returns an optional result from the selected window. Order explicitly for deterministic selection.
func (q ProjectionQuery[S, P]) First(ctx context.Context, executor database.Executor) (value.Optional[P], error) {
	return q.reader().First(ctx, executor)
}

// RequireFirst returns database.NotFound when the selected window is empty.
func (q ProjectionQuery[S, P]) RequireFirst(ctx context.Context, executor database.Executor) (P, error) {
	return q.reader().RequireFirst(ctx, executor)
}

// Count counts the selected result window, including grouping and pagination.
func (q ProjectionQuery[S, P]) Count(ctx context.Context, executor database.Executor) (int64, error) {
	return q.reader().Count(ctx, executor)
}

// Exists reports whether the selected result window contains a row.
func (q ProjectionQuery[S, P]) Exists(ctx context.Context, executor database.Executor) (bool, error) {
	return q.reader().Exists(ctx, executor)
}
