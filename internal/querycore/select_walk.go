package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Visitors may descend into a referenced CTE using the same walker and budget.
// Nested SELECTs are visited unless localSelect restricts discovery to this level;
// traversal does not change scopes.
type SelectWalk struct {
	Named                func(*NamedWindow, int)
	Window               func(WindowNode)
	LocalSelect          bool
	Source               func(TableSource, int)
	Field                func(FieldRef)
	Correlation          func(ScopeRequirement)
	Nodes                int
	Err                  error
	RecursiveRestriction string
}

func (w *SelectWalk) Visit(depth int) bool {
	w.Nodes++
	if depth > MaxExpressionDepth || w.Nodes > MaxExpressionNodes {
		w.Err = fault.New(fault.Invalid, "query analysis exceeds its resource bound")
	}
	return w.Err == nil
}

func (w *SelectWalk) VisitSource(s TableSource, depth int) {
	if !w.Visit(depth) {
		return
	}
	if w.Source != nil {
		w.Source(s, depth)
	}
	if w.LocalSelect {
		return
	}
	if s.Query != nil {
		w.SelectNode(*s.Query, depth+1)
	}
	if s.Set != nil {
		left, right := "", ""
		switch s.Set.Operator {
		case IntersectAllSet:
			left, right = "INTERSECT ALL", "INTERSECT ALL"
		case ExceptAllSet:
			left, right = "EXCEPT ALL", "EXCEPT ALL"
		case ExceptSet:
			right = "right side of EXCEPT"
		}
		w.RestrictRecursion(left, func() { w.SelectNode(s.Set.Left, depth+1) })
		w.RestrictRecursion(right, func() { w.SelectNode(s.Set.Right, depth+1) })
	}
}

func (w *SelectWalk) RestrictRecursion(reason string, visit func()) {
	previous := w.RecursiveRestriction
	if previous == "" {
		w.RecursiveRestriction = reason
	}
	visit()
	w.RecursiveRestriction = previous
}

func (w *SelectWalk) VisitField(f FieldRef) {
	if w.Field != nil {
		w.Field(f)
	}
}

func (w *SelectWalk) SelectNode(s SelectNode, depth int) {
	if !w.Visit(depth) {
		return
	}
	for _, size := range []int{len(s.Joins), len(s.Selections), len(s.Predicates), len(s.Having), len(s.GroupBy), len(s.Orders), len(s.Distinct.Keys), len(s.Locks)} {
		if size > MaxExpressionNodes {
			w.Err = fault.New(fault.Invalid, "query analysis exceeds its resource bound")
			return
		}
	}
	// A RIGHT/FULL join makes the entire preceding join prefix nullable.
	lastNullablePrefix := -1
	for i, j := range s.Joins {
		if j.Kind == RightJoin || j.Kind == FullJoin {
			lastNullablePrefix = i
		}
	}
	visitSource := func(source TableSource, nullable bool) {
		reason := ""
		if nullable {
			reason = "nullable side of an outer join"
		}
		w.RestrictRecursion(reason, func() { w.VisitSource(source, depth) })
	}
	visitSource(s.Source, lastNullablePrefix >= 0)
	for i, j := range s.Joins {
		if w.Err != nil {
			return
		}
		if !w.LocalSelect {
			w.VisitCorrelation(j.Lateral)
		}
		visitSource(j.Source, i < lastNullablePrefix || j.Kind == LeftJoin || j.Kind == FullJoin)
		if j.On != nil || (j.Kind != CrossJoin && j.Lateral == nil) {
			w.Expression(j.On, depth+1)
		}
	}
	for _, s := range s.Selections {
		if w.Err != nil {
			return
		}
		w.Value(s.Expression, depth+1)
	}
	for _, p := range s.Predicates {
		if w.Err != nil {
			return
		}
		w.Expression(p, depth+1)
	}
	for _, p := range s.Having {
		if w.Err != nil {
			return
		}
		w.Expression(p, depth+1)
	}
	for _, f := range s.GroupBy {
		w.Value(f, depth+1)
	}
	for _, f := range s.Distinct.Keys {
		w.Value(f, depth+1)
	}
	for _, o := range s.Orders {
		if w.Err != nil {
			return
		}
		w.Value(o.Expression, depth+1)
	}
}

func (w *SelectWalk) Subquery(q Subquery, depth int) {
	if w.LocalSelect {
		return
	}
	if w.Err != nil {
		return
	}
	if q.Err != nil {
		w.Err = q.Err
		return
	}
	w.VisitCorrelation(q.Correlation)
	w.RestrictRecursion("expression subquery", func() { w.SelectNode(q.Node, depth) })
}

func (w *SelectWalk) VisitCorrelation(required *ScopeRequirement) {
	if required != nil {
		if len(required.Sources) > MaxExpressionNodes {
			w.Err = fault.New(fault.Invalid, "query analysis exceeds its resource bound")
			return
		}
		if w.Correlation != nil {
			w.Correlation(*required)
		}
	}
}

func (w *SelectWalk) Value(v ValueExpression, depth int) {
	if !w.Visit(depth) {
		return
	}
	if handled, err := VisitComputedValue(v, func(v ValueExpression) error { w.Value(v, depth+1); return w.Err }, func(p Expression) error { w.Expression(p, depth+1); return w.Err }); handled {
		w.Err = err
		return
	}
	switch v := v.(type) {
	case FieldRef:
		w.VisitField(v)
	case AggregateNode:
		w.VisitField(v.Field)
		if v.Filter != nil {
			if len(v.Filter.Predicates) > MaxExpressionNodes {
				w.Err = fault.New(fault.Invalid, "aggregate filter analysis exceeds its resource bound")
				return
			}
			for _, p := range v.Filter.Predicates {
				if w.Err != nil {
					return
				}
				w.Expression(p, depth+1)
			}
		}
	case ScalarSubquery:
		w.Subquery(v.Query, depth+1)
	case WindowNode:
		if w.Window != nil {
			w.Window(v)
			if w.Err != nil {
				return
			}
		}
		if v.Err != nil {
			w.Err = v.Err
			return
		}
		if v.Input != nil {
			w.Value(v.Input, depth+1)
		}
		if v.Kind == AggregateWindow {
			w.Value(v.Aggregate, depth+1)
		}
		w.WindowSpec(v.Window, depth+1)
	default:
		w.Err = fault.New(fault.Invalid, "query analysis found an invalid value")
	}
}

func (w *SelectWalk) WindowSpec(spec WindowSpec, depth int) {
	if !w.Visit(depth) {
		return
	}
	err := VisitWindowSpec(spec, func(v ValueExpression) error { w.Value(v, depth+1); return w.Err }, func(d *NamedWindow) error {
		if w.Named != nil {
			w.Named(d, depth)
		} else {
			w.WindowSpec(d.Definition, depth+1)
		}
		return w.Err
	})
	if err != nil {
		w.Err = err
	}
}

func (w *SelectWalk) Expression(e Expression, depth int) {
	if !w.Visit(depth) {
		return
	}
	switch e := e.(type) {
	case RawFragment:
		w.Err = e.Validate()
	case Comparison:
		w.Value(e.Operand, depth+1)
	case BinaryComparison:
		w.Value(e.Left, depth+1)
		w.Value(e.Right, depth+1)
	case RowComparison:
		for _, operand := range e.Operands {
			w.Value(operand, depth+1)
		}
	case ScopeNode:
		// A context scope references only its own validated model table.
	case SubqueryPredicate:
		if e.Operand != nil {
			w.Value(e.Operand, depth+1)
		}
		w.Subquery(e.Query, depth+1)
	case Junction:
		for _, child := range e.Children {
			if w.Err != nil {
				return
			}
			w.Expression(child, depth+1)
		}
	case Negation:
		w.Expression(e.Child, depth+1)
	default:
		w.Err = fault.New(fault.Invalid, "query analysis found an invalid predicate")
	}
}
