package query

import (
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Relation loading qualifies a validated single-model predicate. Only explicit
// correlated outer references follow that rename; isolated inner queries do not.
func requalifyCorrelation(q subquery, alias string) subquery {
	if q.correlation == nil {
		return q
	}
	if len(q.correlation.sources) != 1 {
		q.err = fault.New(fault.Invalid, "model correlation requires one outer source when qualified")
		return q
	}
	for from := range q.correlation.sources {
		r := correlationRenamer{from: from, to: alias}
		q = r.query(q, 0)
		if r.err != nil {
			q.err = r.err
		}
	}
	return q
}

type correlationRenamer struct {
	windows  map[*namedWindow]*namedWindow
	from, to string
	nodes    int
	err      error
}

func (r *correlationRenamer) visit(depth int) bool {
	r.nodes++
	if depth > MaxExpressionDepth || r.nodes > MaxExpressionNodes {
		r.err = fault.New(fault.Invalid, "correlation qualification exceeds its resource bound")
	}
	return r.err == nil
}
func (r *correlationRenamer) field(f fieldRef) fieldRef {
	if f.table == r.from {
		f.table = r.to
	}
	return f
}
func (r *correlationRenamer) query(q subquery, depth int) subquery {
	if q.correlation == nil || q.correlation.sources[r.from] == nil || !r.visit(depth) {
		return q
	}
	if q.node.source.name() == r.from {
		r.err = fault.New(fault.Invalid, "correlation shadows its outer source")
		return q
	}
	for _, j := range q.node.joins {
		if j.source.name() == r.from {
			r.err = fault.New(fault.Invalid, "correlation shadows its outer source")
			return q
		}
	}
	required := *q.correlation
	if r.from != r.to && required.sources[r.to] != nil {
		r.err = fault.New(fault.Invalid, "qualified correlation repeats an outer source")
		return q
	}
	required.sources = maps.Clone(required.sources)
	required.sources[r.to] = required.sources[r.from]
	if r.from != r.to {
		delete(required.sources, r.from)
	}
	q.correlation = &required
	q.node.selections = slices.Clone(q.node.selections)
	for i := range q.node.selections {
		q.node.selections[i].expression = r.value(q.node.selections[i].expression, depth+1)
	}
	q.node.orders = slices.Clone(q.node.orders)
	for i := range q.node.orders {
		q.node.orders[i].expression = r.value(q.node.orders[i].expression, depth+1)
	}
	q.node.groupBy = slices.Clone(q.node.groupBy)
	for i := range q.node.groupBy {
		q.node.groupBy[i] = r.value(q.node.groupBy[i], depth+1)
	}
	q.node.distinct.keys = slices.Clone(q.node.distinct.keys)
	for i := range q.node.distinct.keys {
		q.node.distinct.keys[i] = r.value(q.node.distinct.keys[i], depth+1)
	}
	q.node.predicates = r.expressions(q.node.predicates, depth+1)
	q.node.having = r.expressions(q.node.having, depth+1)
	q.node.joins = slices.Clone(q.node.joins)
	for i := range q.node.joins {
		j := &q.node.joins[i]
		if j.on != nil {
			j.on = r.expression(j.on, depth+1)
		}
		if j.lateral != nil && j.source.query != nil {
			nested := r.query(subquery{node: *j.source.query, correlation: j.lateral}, depth+1)
			j.source.query, j.lateral = &nested.node, nested.correlation
		}
	}
	return q
}
func (r *correlationRenamer) value(v valueExpression, depth int) valueExpression {
	if !r.visit(depth) {
		return v
	}
	if mapped, ok := mapComputedValue(v, func(v valueExpression) valueExpression { return r.value(v, depth+1) }, func(p expression) expression { return r.expression(p, depth+1) }); ok {
		return mapped
	}
	switch v := v.(type) {
	case fieldRef:
		return r.field(v)
	case aggregateNode:
		v.field = r.field(v.field)
		if v.filter != nil {
			v.filter = &aggregateFilter{predicates: r.expressions(v.filter.predicates, depth+1)}
		}
		return v
	case scalarSubquery:
		v.query = r.query(v.query, depth+1)
		return v
	case windowNode:
		if v.input != nil {
			v.input = r.value(v.input, depth+1)
		}
		if v.kind == aggregateWindow {
			v.aggregate = r.value(v.aggregate, depth+1).(aggregateNode)
		}
		v.window = r.windowSpec(v.window, depth+1)
		return v
	default:
		return v
	}
}
func (r *correlationRenamer) windowSpec(w windowSpec, depth int) windowSpec {
	if !r.visit(depth) {
		return w
	}
	if w.reference != nil {
		if r.windows == nil {
			r.windows = make(map[*namedWindow]*namedWindow)
		}
		original := w.reference
		if replacement := r.windows[original]; replacement != nil {
			w.reference = replacement
		} else {
			replacement := *original
			r.windows[original] = &replacement
			replacement.definition = r.windowSpec(original.definition, depth+1)
			w.reference = &replacement
		}
	}
	w.partitions = slices.Clone(w.partitions)
	for i, v := range w.partitions {
		w.partitions[i] = r.value(v, depth+1)
	}
	w.orders = slices.Clone(w.orders)
	for i, o := range w.orders {
		w.orders[i].expression = r.value(o.expression, depth+1)
	}
	return w
}
func (r *correlationRenamer) expressions(items []expression, depth int) []expression {
	result := slices.Clone(items)
	for i := range result {
		result[i] = r.expression(result[i], depth)
	}
	return result
}
func (r *correlationRenamer) expression(e expression, depth int) expression {
	if !r.visit(depth) {
		return e
	}
	switch e := e.(type) {
	case comparison:
		e.operand = r.value(e.operand, depth+1)
		return e
	case binaryComparison:
		e.left, e.right = r.value(e.left, depth+1), r.value(e.right, depth+1)
		return e
	case subqueryPredicate:
		if e.operand != nil {
			e.operand = r.value(e.operand, depth+1)
		}
		e.query = r.query(e.query, depth+1)
		return e
	case junction:
		e.children = r.expressions(e.children, depth+1)
		return e
	case negation:
		return negation{r.expression(e.child, depth+1)}
	default:
		return e
	}
}
