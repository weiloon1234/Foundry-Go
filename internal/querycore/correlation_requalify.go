package querycore

import (
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Relation loading qualifies a validated single-model predicate. Only explicit
// correlated outer references follow that rename; isolated inner queries do not.
func RequalifyCorrelation(q Subquery, alias string) Subquery {
	if q.Correlation == nil {
		return q
	}
	if len(q.Correlation.Sources) != 1 {
		q.Err = fault.New(fault.Invalid, "model correlation requires one outer source when qualified")
		return q
	}
	for from := range q.Correlation.Sources {
		r := correlationRenamer{from: from, to: alias}
		q = r.query(q, 0)
		if r.err != nil {
			q.Err = r.err
		}
	}
	return q
}

type correlationRenamer struct {
	windows  map[*NamedWindow]*NamedWindow
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

func (r *correlationRenamer) field(f FieldRef) FieldRef {
	if f.Table == r.from {
		f.Table = r.to
	}
	return f
}

func (r *correlationRenamer) query(q Subquery, depth int) Subquery {
	if q.Correlation == nil || q.Correlation.Sources[r.from] == nil || !r.visit(depth) {
		return q
	}
	if q.Node.Source.Name() == r.from {
		r.err = fault.New(fault.Invalid, "correlation shadows its outer source")
		return q
	}
	for _, j := range q.Node.Joins {
		if j.Source.Name() == r.from {
			r.err = fault.New(fault.Invalid, "correlation shadows its outer source")
			return q
		}
	}
	required := *q.Correlation
	if r.from != r.to && required.Sources[r.to] != nil {
		r.err = fault.New(fault.Invalid, "qualified correlation repeats an outer source")
		return q
	}
	required.Sources = maps.Clone(required.Sources)
	required.Sources[r.to] = required.Sources[r.from]
	if r.from != r.to {
		delete(required.Sources, r.from)
	}
	q.Correlation = &required
	q.Node.Selections = slices.Clone(q.Node.Selections)
	for i := range q.Node.Selections {
		q.Node.Selections[i].Expression = r.value(q.Node.Selections[i].Expression, depth+1)
	}
	q.Node.Orders = slices.Clone(q.Node.Orders)
	for i := range q.Node.Orders {
		q.Node.Orders[i].Expression = r.value(q.Node.Orders[i].Expression, depth+1)
	}
	q.Node.GroupBy = slices.Clone(q.Node.GroupBy)
	for i := range q.Node.GroupBy {
		q.Node.GroupBy[i] = r.value(q.Node.GroupBy[i], depth+1)
	}
	q.Node.Distinct.Keys = slices.Clone(q.Node.Distinct.Keys)
	for i := range q.Node.Distinct.Keys {
		q.Node.Distinct.Keys[i] = r.value(q.Node.Distinct.Keys[i], depth+1)
	}
	q.Node.Predicates = r.expressions(q.Node.Predicates, depth+1)
	q.Node.Having = r.expressions(q.Node.Having, depth+1)
	q.Node.Joins = slices.Clone(q.Node.Joins)
	for i := range q.Node.Joins {
		j := &q.Node.Joins[i]
		if j.On != nil {
			j.On = r.expression(j.On, depth+1)
		}
		if j.Lateral != nil && j.Source.Query != nil {
			nested := r.query(Subquery{Node: *j.Source.Query, Correlation: j.Lateral}, depth+1)
			j.Source.Query, j.Lateral = &nested.Node, nested.Correlation
		}
	}
	return q
}

func (r *correlationRenamer) value(v ValueExpression, depth int) ValueExpression {
	if !r.visit(depth) {
		return v
	}
	if mapped, ok := MapComputedValue(v, func(v ValueExpression) ValueExpression { return r.value(v, depth+1) }, func(p Expression) Expression { return r.expression(p, depth+1) }); ok {
		return mapped
	}
	switch v := v.(type) {
	case FieldRef:
		return r.field(v)
	case AggregateNode:
		v.Field = r.field(v.Field)
		if v.Filter != nil {
			v.Filter = &AggregateFilter{Predicates: r.expressions(v.Filter.Predicates, depth+1)}
		}
		return v
	case ScalarSubquery:
		v.Query = r.query(v.Query, depth+1)
		return v
	case WindowNode:
		if v.Input != nil {
			v.Input = r.value(v.Input, depth+1)
		}
		if v.Kind == AggregateWindow {
			v.Aggregate = r.value(v.Aggregate, depth+1).(AggregateNode)
		}
		v.Window = r.windowSpec(v.Window, depth+1)
		return v
	default:
		return v
	}
}

func (r *correlationRenamer) windowSpec(w WindowSpec, depth int) WindowSpec {
	if !r.visit(depth) {
		return w
	}
	if w.Reference != nil {
		if r.windows == nil {
			r.windows = make(map[*NamedWindow]*NamedWindow)
		}
		original := w.Reference
		if replacement := r.windows[original]; replacement != nil {
			w.Reference = replacement
		} else {
			replacement := *original
			r.windows[original] = &replacement
			replacement.Definition = r.windowSpec(original.Definition, depth+1)
			w.Reference = &replacement
		}
	}
	w.Partitions = slices.Clone(w.Partitions)
	for i, v := range w.Partitions {
		w.Partitions[i] = r.value(v, depth+1)
	}
	w.Orders = slices.Clone(w.Orders)
	for i, o := range w.Orders {
		w.Orders[i].Expression = r.value(o.Expression, depth+1)
	}
	return w
}

func (r *correlationRenamer) expressions(items []Expression, depth int) []Expression {
	result := slices.Clone(items)
	for i := range result {
		result[i] = r.expression(result[i], depth)
	}
	return result
}

func (r *correlationRenamer) expression(e Expression, depth int) Expression {
	if !r.visit(depth) {
		return e
	}
	switch e := e.(type) {
	case Comparison:
		e.Operand = r.value(e.Operand, depth+1)
		return e
	case BinaryComparison:
		e.Left, e.Right = r.value(e.Left, depth+1), r.value(e.Right, depth+1)
		return e
	case RowComparison:
		operands := make([]ValueExpression, len(e.Operands))
		for i, operand := range e.Operands {
			operands[i] = r.value(operand, depth+1)
		}
		e.Operands = operands
		return e
	case ScopeNode:
		from, to := r.from, r.to
		return e.Rewrite(func(resolved Expression) (Expression, error) {
			renamer := correlationRenamer{from: from, to: to}
			renamed := renamer.expression(resolved, depth+1)
			return renamed, renamer.err
		})
	case SubqueryPredicate:
		if e.Operand != nil {
			e.Operand = r.value(e.Operand, depth+1)
		}
		e.Query = r.query(e.Query, depth+1)
		return e
	case Junction:
		e.Children = r.expressions(e.Children, depth+1)
		return e
	case Negation:
		return Negation{Child: r.expression(e.Child, depth+1)}
	default:
		return e
	}
}
