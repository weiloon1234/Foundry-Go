package query

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// RelatedValue computes a typed aggregate over a relationship's matching
// inputs for each source row, without a model slot: a correlated scalar
// subquery using the relationship's keys, target/pivot filters and global
// scopes. An aggregate without grouping always yields one row, so V keeps the
// computation's own nullability (COUNT is never NULL; SUM of no rows is NULL).
//
//	orders := query.RelatedValue(UserRelations().Orders, query.Count[Order]())
//	busiest := QueryUsers().OrderBy(orders.Desc())          // order by relation count
//	active := QueryUsers().Where(query.OrderRow(orders).Gte(3))
//	pairs, err := query.WithValue(QueryUsers(), orders).All(ctx, db)
func RelatedValue[M, N, V any](source AggregateSource[M, N], computation AggregateExpression[N, V]) RowExpression[M, V] {
	if nilDescriptor(source) || nilDescriptor(computation) {
		return RowExpression[M, V]{Expression[M, V]{node: parameterNode{Err: fault.New(fault.Invalid, "related value requires a relationship and computation")}}}
	}
	aggregate := computation.aggregateValue()
	input := source.aggregateInput()
	result := RowExpression[M, V]{Expression[M, V]{codec: aggregate.codec}}
	node, err := relatedValueNode(input, aggregate.node)
	if err != nil {
		result.expression.node = parameterNode{Err: err}
		return result
	}
	result.expression.node = scalarSubquery{Query: subquery{Node: node, Correlation: &scopeRequirement{
		Sources: map[string][]columnData{input.source.queryCore.Table: input.source.definition.modelDefinition.Columns},
	}}}
	return result
}

// relatedValueNode builds SELECT <aggregate> FROM <inputs> WHERE <filters>
// AND <input key> = <outer source key>. Direct relations alias their target so
// self-relations stay unambiguous; many-to-many inputs keep their join aliases.
func relatedValueNode[M, N any](input aggregateInput[M, N], measure aggregateNode) (selectNode, error) {
	if input.validate == nil || input.selectNode == nil || input.source.definition == nil {
		return selectNode{}, fault.New(fault.Invalid, "related value requires a bound relationship")
	}
	if err := input.validate(0, DefaultRelationLimits()); err != nil {
		return selectNode{}, err
	}
	field := func(f fieldRef) error { return f.Validate(input.inputTable) }
	if err := measure.Validate(field); err != nil {
		return selectNode{}, err
	}
	nodes := 0
	if err := measure.ValidateFilter(field, &nodes); err != nil {
		return selectNode{}, err
	}
	node := input.selectNode()
	node.Selections, node.Orders, node.GroupBy = nil, nil, nil
	alias, group := input.inputAlias, input.group
	if input.inputAlias == input.inputTable {
		names, err := namesInSelects(node)
		if err != nil {
			return selectNode{}, err
		}
		names.Used[input.source.queryCore.Table] = true
		alias = names.Allocate("foundry_related")
		node.Source.Alias = alias
		predicates := make([]expression, len(node.Predicates))
		for i, predicate := range node.Predicates {
			predicates[i] = requalify(predicate, alias)
		}
		node.Predicates = predicates
		group = fieldRef{Table: alias, Column: input.group.Column}
	}
	node.Selections = []selectItem{{Expression: requalifyAggregate(measure, alias)}}
	node.Predicates = append(node.Predicates, binaryComparison{Left: group, Right: input.local, Operator: equal})
	return node, nil
}

// Annotated pairs a complete model with one computed value from the same row.
type Annotated[M, V any] struct {
	Model M
	Value V
}

// AnnotatedQuery reads complete models together with one computed row value.
type AnnotatedQuery[M, V any] struct {
	query Query[M]
	value RowExpression[M, V]
}

// WithValue reads each model with a computed value, such as a RelatedValue
// count, in the same statement. The query keeps its filters, scopes, order,
// window and eager relations; retrieval hooks run for the models as usual.
func WithValue[M, V any](q Query[M], computed RowExpression[M, V]) AnnotatedQuery[M, V] {
	return AnnotatedQuery[M, V]{query: q, value: computed}
}

func (q AnnotatedQuery[M, V]) reader() readResult[Annotated[M, V]] {
	r := readResult[Annotated[M, V]]{}
	if r.err = q.query.Validate(); r.err != nil {
		return r
	}
	definition := q.query.definition
	if definition == nil || q.value.expression.node == nil || q.value.expression.codec.Validate() != nil {
		r.err = fault.New(fault.Invalid, "annotated reads require model metadata and a typed value")
		return r
	}
	r.node = q.query.modelSelect()
	r.node.Selections = append(r.node.Selections, selectItem{Expression: q.value.expression.node, Alias: "foundry_value"})
	columns := len(definition.modelDefinition.Columns)
	codec := q.value.expression.codec
	r.scan = func(row database.Row) (Annotated[M, V], error) {
		model, err := definition.scan(partitionedRow{row, 0, columns, columns + 1})
		if err != nil {
			return Annotated[M, V]{}, err
		}
		var computed V
		if err := (partitionedRow{row, columns, 1, columns + 1}).Scan(codec.Scan(&computed)); err != nil {
			return Annotated[M, V]{}, err
		}
		return Annotated[M, V]{Model: model, Value: computed}, nil
	}
	models := definition.retrieval()
	r.lifecycle = &readLifecycle[Annotated[M, V]]{
		primaryIndex: -1,
		needed:       models.needed,
		prepare: func(ctx context.Context, set lifecycle.Observers) (func(context.Context, database.Executor, Annotated[M, V]) error, error) {
			invoke, err := models.prepare(ctx, set)
			if err != nil || invoke == nil {
				return nil, err
			}
			return func(ctx context.Context, executor database.Executor, item Annotated[M, V]) error {
				return invoke(ctx, executor, item.Model)
			}, nil
		},
	}
	return r
}

// Compile validates the annotated SELECT without executing it.
func (q AnnotatedQuery[M, V]) Compile() (Statement, error) { return q.reader().Compile() }

// All reads every annotated model, then loads the query's eager relations.
func (q AnnotatedQuery[M, V]) All(ctx context.Context, executor database.Executor) ([]Annotated[M, V], error) {
	items, err := q.reader().All(ctx, executor)
	if err != nil || len(q.query.relations) == 0 || len(items) == 0 {
		return items, err
	}
	models := make([]M, len(items))
	for i, item := range items {
		models[i] = item.Model
	}
	loaded, err := q.query.Load(ctx, executor, models)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Model = loaded[i]
	}
	return items, nil
}
