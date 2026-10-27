package querycore

import (
	"maps"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Relation loading qualifies a validated single-model predicate. Only explicit
// correlated outer references follow that rename; isolated inner queries do not.
func RequalifyCorrelation(q Subquery, alias string) Subquery {
	if q.Correlation == nil {
		return q
	}
	if len(q.Correlation.Sources) != 1 {
		q.Err = safeerror.NewFault(fault.Invalid, "model correlation requires one outer source when qualified")
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
		r.err = safeerror.NewFault(fault.Invalid, "correlation qualification exceeds its resource bound")
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
		r.err = safeerror.NewFault(fault.Invalid, "correlation shadows its outer source")
		return q
	}
	for _, j := range q.Node.Joins {
		if j.Source.Name() == r.from {
			r.err = safeerror.NewFault(fault.Invalid, "correlation shadows its outer source")
			return q
		}
	}
	required := *q.Correlation
	if r.from != r.to && required.Sources[r.to] != nil {
		r.err = safeerror.NewFault(fault.Invalid, "qualified correlation repeats an outer source")
		return q
	}
	required.Sources = maps.Clone(required.Sources)
	required.Sources[r.to] = required.Sources[r.from]
	if r.from != r.to {
		delete(required.Sources, r.from)
	}
	q.Correlation = &required
	q.Node = r.avoidCapture(q.Node, required, depth)
	q.Node = r.selectNode(q.Node, depth)
	return q
}

// Deferred scopes can introduce a source after the enclosing relation chose its
// alias. Rename that local source before substituting the outer name; rejecting
// the original shadowing above still protects malformed correlations.
func (r *correlationRenamer) avoidCapture(node SelectNode, required ScopeRequirement, depth int) SelectNode {
	count := 0
	if node.Source.Name() == r.to {
		count++
	}
	for _, join := range node.Joins {
		if join.Source.Name() == r.to {
			count++
		}
	}
	if count == 0 || r.err != nil {
		return node
	}
	if count != 1 {
		r.err = safeerror.NewFault(fault.Invalid, "invalid or repeated select source")
		return node
	}
	names, err := NamesInSelects(node)
	if err != nil {
		r.err = err
		return node
	}
	names.Used[r.from], names.Used[r.to] = true, true
	for name := range required.Sources {
		names.Used[name] = true
	}
	alias := names.Allocate(r.to)
	local := correlationRenamer{from: r.to, to: alias, nodes: r.nodes}
	node = local.selectNode(node, depth)
	r.nodes, r.err = local.nodes, local.err
	if node.Source.Name() == r.to {
		node.Source.Alias = alias
	}
	for i := range node.Joins {
		if node.Joins[i].Source.Name() == r.to {
			node.Joins[i].Source.Alias = alias
		}
	}
	node.Locks = slices.Clone(node.Locks)
	for i := range node.Locks {
		node.Locks[i].Targets = slices.Clone(node.Locks[i].Targets)
		for j, target := range node.Locks[i].Targets {
			if target == r.to {
				node.Locks[i].Targets[j] = alias
			}
		}
	}
	return node
}

func (r *correlationRenamer) selectNode(node SelectNode, depth int) SelectNode {
	node.Selections = slices.Clone(node.Selections)
	for i := range node.Selections {
		node.Selections[i].Expression = r.value(node.Selections[i].Expression, depth+1)
	}
	node.Orders = slices.Clone(node.Orders)
	for i := range node.Orders {
		node.Orders[i].Expression = r.value(node.Orders[i].Expression, depth+1)
	}
	node.GroupBy = slices.Clone(node.GroupBy)
	for i := range node.GroupBy {
		node.GroupBy[i] = r.value(node.GroupBy[i], depth+1)
	}
	node.Distinct.Keys = slices.Clone(node.Distinct.Keys)
	for i := range node.Distinct.Keys {
		node.Distinct.Keys[i] = r.value(node.Distinct.Keys[i], depth+1)
	}
	node.Predicates = r.expressions(node.Predicates, depth+1)
	node.Having = r.expressions(node.Having, depth+1)
	node.Joins = slices.Clone(node.Joins)
	for i := range node.Joins {
		j := &node.Joins[i]
		if j.On != nil {
			j.On = r.expression(j.On, depth+1)
		}
		if j.Lateral != nil && j.Source.Query != nil {
			nested := r.query(Subquery{Node: *j.Source.Query, Correlation: j.Lateral}, depth+1)
			j.Source.Query, j.Lateral = &nested.Node, nested.Correlation
		}
	}
	return node
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
