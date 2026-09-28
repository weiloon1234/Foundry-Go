package query

import "github.com/weiloon1234/Foundry-Go/fault"

// Visitors may descend into a referenced CTE using the same walker and budget.
// Nested SELECTs are visited unless localSelect restricts discovery to this level;
// traversal does not change scopes.
type selectWalk struct {
	named                func(*namedWindow, int)
	window               func(windowNode)
	localSelect          bool
	source               func(tableSource, int)
	field                func(fieldRef)
	correlation          func(scopeRequirement)
	nodes                int
	err                  error
	recursiveRestriction string
}

func (w *selectWalk) visit(depth int) bool {
	w.nodes++
	if depth > MaxExpressionDepth || w.nodes > MaxExpressionNodes {
		w.err = fault.New(fault.Invalid, "query analysis exceeds its resource bound")
	}
	return w.err == nil
}
func (w *selectWalk) visitSource(s tableSource, depth int) {
	if !w.visit(depth) {
		return
	}
	if w.source != nil {
		w.source(s, depth)
	}
	if w.localSelect {
		return
	}
	if s.query != nil {
		w.selectNode(*s.query, depth+1)
	}
	if s.set != nil {
		left, right := "", ""
		switch s.set.operator {
		case intersectAllSet:
			left, right = "INTERSECT ALL", "INTERSECT ALL"
		case exceptAllSet:
			left, right = "EXCEPT ALL", "EXCEPT ALL"
		case exceptSet:
			right = "right side of EXCEPT"
		}
		w.restrictRecursion(left, func() { w.selectNode(s.set.left, depth+1) })
		w.restrictRecursion(right, func() { w.selectNode(s.set.right, depth+1) })
	}
}

func (w *selectWalk) restrictRecursion(reason string, visit func()) {
	previous := w.recursiveRestriction
	if previous == "" {
		w.recursiveRestriction = reason
	}
	visit()
	w.recursiveRestriction = previous
}
func (w *selectWalk) visitField(f fieldRef) {
	if w.field != nil {
		w.field(f)
	}
}
func (w *selectWalk) selectNode(s selectNode, depth int) {
	if !w.visit(depth) {
		return
	}
	for _, size := range []int{len(s.joins), len(s.selections), len(s.predicates), len(s.having), len(s.groupBy), len(s.orders), len(s.distinct.keys), len(s.locks)} {
		if size > MaxExpressionNodes {
			w.err = fault.New(fault.Invalid, "query analysis exceeds its resource bound")
			return
		}
	}
	// A RIGHT/FULL join makes the entire preceding join prefix nullable.
	lastNullablePrefix := -1
	for i, j := range s.joins {
		if j.kind == rightJoin || j.kind == fullJoin {
			lastNullablePrefix = i
		}
	}
	visitSource := func(source tableSource, nullable bool) {
		reason := ""
		if nullable {
			reason = "nullable side of an outer join"
		}
		w.restrictRecursion(reason, func() { w.visitSource(source, depth) })
	}
	visitSource(s.source, lastNullablePrefix >= 0)
	for i, j := range s.joins {
		if w.err != nil {
			return
		}
		if !w.localSelect {
			w.visitCorrelation(j.lateral)
		}
		visitSource(j.source, i < lastNullablePrefix || j.kind == leftJoin || j.kind == fullJoin)
		if j.on != nil || (j.kind != crossJoin && j.lateral == nil) {
			w.expression(j.on, depth+1)
		}
	}
	for _, s := range s.selections {
		if w.err != nil {
			return
		}
		w.value(s.expression, depth+1)
	}
	for _, p := range s.predicates {
		if w.err != nil {
			return
		}
		w.expression(p, depth+1)
	}
	for _, p := range s.having {
		if w.err != nil {
			return
		}
		w.expression(p, depth+1)
	}
	for _, f := range s.groupBy {
		w.value(f, depth+1)
	}
	for _, f := range s.distinct.keys {
		w.value(f, depth+1)
	}
	for _, o := range s.orders {
		if w.err != nil {
			return
		}
		w.value(o.expression, depth+1)
	}
}
func (w *selectWalk) subquery(q subquery, depth int) {
	if w.localSelect {
		return
	}
	if w.err != nil {
		return
	}
	if q.err != nil {
		w.err = q.err
		return
	}
	w.visitCorrelation(q.correlation)
	w.restrictRecursion("expression subquery", func() { w.selectNode(q.node, depth) })
}
func (w *selectWalk) visitCorrelation(required *scopeRequirement) {
	if required != nil {
		if len(required.sources) > MaxExpressionNodes {
			w.err = fault.New(fault.Invalid, "query analysis exceeds its resource bound")
			return
		}
		if w.correlation != nil {
			w.correlation(*required)
		}
	}
}
func (w *selectWalk) value(v valueExpression, depth int) {
	if !w.visit(depth) {
		return
	}
	if handled, err := visitComputedValue(v, func(v valueExpression) error { w.value(v, depth+1); return w.err }, func(p expression) error { w.expression(p, depth+1); return w.err }); handled {
		w.err = err
		return
	}
	switch v := v.(type) {
	case fieldRef:
		w.visitField(v)
	case aggregateNode:
		w.visitField(v.field)
		if v.filter != nil {
			if len(v.filter.predicates) > MaxExpressionNodes {
				w.err = fault.New(fault.Invalid, "aggregate filter analysis exceeds its resource bound")
				return
			}
			for _, p := range v.filter.predicates {
				if w.err != nil {
					return
				}
				w.expression(p, depth+1)
			}
		}
	case scalarSubquery:
		w.subquery(v.query, depth+1)
	case windowNode:
		if w.window != nil {
			w.window(v)
			if w.err != nil {
				return
			}
		}
		if v.err != nil {
			w.err = v.err
			return
		}
		if v.input != nil {
			w.value(v.input, depth+1)
		}
		if v.kind == aggregateWindow {
			w.value(v.aggregate, depth+1)
		}
		w.windowSpec(v.window, depth+1)
	default:
		w.err = fault.New(fault.Invalid, "query analysis found an invalid value")
	}
}
func (w *selectWalk) windowSpec(spec windowSpec, depth int) {
	if !w.visit(depth) {
		return
	}
	err := visitWindowSpec(spec, func(v valueExpression) error { w.value(v, depth+1); return w.err }, func(d *namedWindow) error {
		if w.named != nil {
			w.named(d, depth)
		} else {
			w.windowSpec(d.definition, depth+1)
		}
		return w.err
	})
	if err != nil {
		w.err = err
	}
}
func (w *selectWalk) expression(e expression, depth int) {
	if !w.visit(depth) {
		return
	}
	switch e := e.(type) {
	case comparison:
		w.value(e.operand, depth+1)
	case binaryComparison:
		w.value(e.left, depth+1)
		w.value(e.right, depth+1)
	case subqueryPredicate:
		if e.operand != nil {
			w.value(e.operand, depth+1)
		}
		w.subquery(e.query, depth+1)
	case junction:
		for _, child := range e.children {
			if w.err != nil {
				return
			}
			w.expression(child, depth+1)
		}
	case negation:
		w.expression(e.child, depth+1)
	default:
		w.err = fault.New(fault.Invalid, "query analysis found an invalid predicate")
	}
}
