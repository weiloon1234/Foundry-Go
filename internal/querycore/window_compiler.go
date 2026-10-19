package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (c *Compiler) ValidateWindow(n WindowNode, grouped map[FieldRef]bool, grouping bool) error {
	if c.InWindow {
		return fault.New(fault.Invalid, "window functions cannot nest at the same SELECT level")
	}
	if n.Err != nil {
		return n.Err
	}
	if n.Window.Err != nil {
		return n.Window.Err
	}
	resolved, err := ResolveWindow(n.Window, false, 0)
	if err != nil {
		return err
	}
	n.Window = resolved
	if len(n.Window.Partitions) > MaxExpressionNodes || len(n.Window.Orders) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "window exceeds its resource bound")
	}
	previous := c.InWindow
	c.InWindow = true
	defer func() { c.InWindow = previous }()
	check := func(v ValueExpression) error { return c.ValidateSelectedExpression(v, grouped, grouping) }
	if n.Kind != AggregateWindow && n.Aggregate != (AggregateNode{}) {
		return fault.New(fault.Invalid, "window function has an unexpected aggregate")
	}
	needsValue := n.Kind >= LagWindow && n.Kind <= NthValueWindow
	if needsValue {
		if err := check(n.Input); err != nil {
			return err
		}
	} else if n.Input != nil {
		return fault.New(fault.Invalid, "window function has an unexpected value")
	}
	switch n.Kind {
	case RowNumberWindow, RankWindow, DenseRankWindow, PercentRankWindow, CumeDistWindow, FirstValueWindow, LastValueWindow:
		if n.N != 0 {
			return fault.New(fault.Invalid, "window function has an unexpected position")
		}
	case NtileWindow, NthValueWindow:
		if n.N <= 0 {
			return fault.New(fault.Invalid, "window position or bucket count must be positive")
		}
	case LagWindow, LeadWindow:
		// PostgreSQL permits negative offsets (the opposite direction).
	case AggregateWindow:
		if n.N != 0 || n.Aggregate.Kind == CountDistinct {
			return fault.New(fault.Invalid, "unsupported aggregate window declaration")
		}
		if err := n.Aggregate.Validate(func(f FieldRef) error { return check(f) }); err != nil {
			return err
		}
		if err := n.Aggregate.ValidateFilter(func(f FieldRef) error { return check(f) }, &c.ExpressionNodes); err != nil {
			return err
		}
	default:
		return fault.New(fault.Invalid, "invalid window function")
	}
	if (n.HasFallback || n.Fallback != nil) && n.Kind != LagWindow && n.Kind != LeadWindow {
		return fault.New(fault.Invalid, "only lag/lead accept fallback values")
	}
	if !n.HasFallback && n.Fallback != nil {
		return fault.New(fault.Invalid, "unexpected window fallback")
	}
	var seen ValueIndex
	for _, key := range n.Window.Partitions {
		if err := check(key); err != nil {
			return err
		}
		plan, err := c.PlanValue(key)
		if err != nil {
			return err
		}
		_, found, err := seen.Find(plan)
		if err != nil {
			return err
		}
		if found {
			return fault.New(fault.Invalid, "repeated window partition key")
		}
		seen.Add(plan)
	}
	for _, order := range n.Window.Orders {
		if err := check(order.Expression); err != nil {
			return err
		}
	}
	return n.Window.ValidateFrame()
}

func (w WindowSpec) ValidateFrame() error {
	f := w.Frame
	if f == nil {
		return nil
	}
	if f.Kind < RowsFrame || f.Kind > RangeFrame || f.Exclusion > ExcludeTies {
		return fault.New(fault.Invalid, "invalid window frame kind or exclusion")
	}
	if f.Kind == GroupsFrame && len(w.Orders) == 0 {
		return fault.New(fault.Invalid, "GROUPS frame requires window ordering")
	}
	if f.RangeType != 0 {
		if f.Kind != RangeFrame || len(w.Orders) != 1 {
			return fault.New(fault.Invalid, "value-distance RANGE requires exactly one ordering expression")
		}
		switch f.RangeType {
		case codec.TypeInteger, codec.TypeFloat, codec.TypeDecimal, codec.TypeDate, codec.TypeTime, codec.TypeDateTime, codec.TypeLocalDateTime:
		default:
			return fault.New(fault.Invalid, "unsupported RANGE ordering representation")
		}
	}
	for _, bound := range []FrameBound{f.Start, f.End} {
		switch bound.Kind {
		case UnboundedPreceding, CurrentRow, UnboundedFollowing:
			if bound.Offset != 0 || bound.Distance != nil {
				return fault.New(fault.Invalid, "frame position has an unexpected offset")
			}
		case PrecedingBound, FollowingBound:
			if f.Kind == RangeFrame {
				if bound.Offset != 0 || bound.Distance == nil || f.RangeType == 0 {
					return fault.New(fault.Invalid, "RANGE offset requires a typed distance")
				}
				if err := bound.Distance.Validate(f.RangeType); err != nil {
					return err
				}
			} else if bound.Offset < 0 || bound.Distance != nil {
				return fault.New(fault.Invalid, "frame offset requires a nonnegative ROWS/GROUPS distance")
			}
		default:
			return fault.New(fault.Invalid, "invalid window frame boundary")
		}
	}
	if f.Start.Kind == UnboundedFollowing || f.End.Kind == UnboundedPreceding || f.End.Kind < f.Start.Kind {
		return fault.New(fault.Invalid, "invalid window frame boundary order")
	}
	return nil
}

func (c *Compiler) WindowSQL(n WindowNode, grouped map[FieldRef]bool, grouping bool) (string, error) {
	previous := c.InWindow
	c.InWindow = true
	defer func() { c.InWindow = previous }()
	var call string
	if n.Kind == AggregateWindow {
		a := n.Aggregate
		if a.Kind == ExistsValues {
			a.Kind = CountAll
		}
		var err error
		call, err = c.AggregateAt(a, func(f FieldRef) error { return c.ValidateSelectedExpression(f, grouped, grouping) }, grouped, grouping, true)
		if err != nil {
			return "", err
		}
	} else {
		names := [...]string{"", "ROW_NUMBER", "RANK", "DENSE_RANK", "PERCENT_RANK", "CUME_DIST", "NTILE", "LAG", "LEAD", "FIRST_VALUE", "LAST_VALUE", "NTH_VALUE"}
		if n.Kind < RowNumberWindow || int(n.Kind) >= len(names) {
			return "", fault.New(fault.Invalid, "invalid window function")
		}
		var args []string
		if n.Input != nil {
			text, err := c.SelectedExpression(n.Input, grouped, grouping)
			if err != nil {
				return "", err
			}
			args = append(args, text)
		}
		if n.Kind == NtileWindow || n.Kind == LagWindow || n.Kind == LeadWindow || n.Kind == NthValueWindow {
			p, err := c.Parameter(int64(n.N))
			if err != nil {
				return "", err
			}
			args = append(args, p)
		}
		if n.HasFallback {
			p, err := c.Parameter(n.Fallback)
			if err != nil {
				return "", err
			}
			args = append(args, p)
		}
		call = names[n.Kind] + "(" + strings.Join(args, ", ") + ")"
	}
	spec, err := c.WindowSpecSQL(n.Window, grouped, grouping)
	if err != nil {
		return "", err
	}
	result := call + " OVER (" + spec + ")"
	if n.Window.ReferenceOnly() {
		result = call + " OVER " + Quoted(n.Window.Reference.Name)
	}
	if n.Kind == AggregateWindow && n.Aggregate.Kind == ExistsValues {
		result = "(" + result + " > 0)"
	}
	return result, nil
}

func (c *Compiler) WindowSpecSQL(w WindowSpec, grouped map[FieldRef]bool, grouping bool) (string, error) {
	var parts []string
	if w.Reference != nil {
		parts = append(parts, Quoted(w.Reference.Name))
	}
	if len(w.Partitions) != 0 {
		names := make([]string, len(w.Partitions))
		for i, f := range w.Partitions {
			text, err := c.SelectedExpression(f, grouped, grouping)
			if err != nil {
				return "", err
			}
			names[i] = text
		}
		parts = append(parts, "PARTITION BY "+strings.Join(names, ", "))
	}
	if len(w.Orders) != 0 {
		names := make([]string, len(w.Orders))
		for i, order := range w.Orders {
			text, err := c.SelectedExpression(order.Expression, grouped, grouping)
			if err != nil {
				return "", err
			}
			names[i] = text + OrderDirection(order)
		}
		parts = append(parts, "ORDER BY "+strings.Join(names, ", "))
	}
	if f := w.Frame; f != nil {
		start, err := c.FrameBoundSQL(f.Start)
		if err != nil {
			return "", err
		}
		end, err := c.FrameBoundSQL(f.End)
		if err != nil {
			return "", err
		}
		name := [...]string{"", "ROWS", "GROUPS", "RANGE"}[f.Kind]
		text := name + " BETWEEN " + start + " AND " + end
		text += [...]string{"", " EXCLUDE CURRENT ROW", " EXCLUDE GROUP", " EXCLUDE TIES"}[f.Exclusion]
		parts = append(parts, text)
	}
	return strings.Join(parts, " "), nil
}

func (c *Compiler) FrameBoundSQL(b FrameBound) (string, error) {
	switch b.Kind {
	case UnboundedPreceding:
		return "UNBOUNDED PRECEDING", nil
	case CurrentRow:
		return "CURRENT ROW", nil
	case UnboundedFollowing:
		return "UNBOUNDED FOLLOWING", nil
	case PrecedingBound, FollowingBound:
		var p string
		var err error
		if b.Distance != nil {
			p, err = c.RangeDistanceSQL(*b.Distance)
		} else {
			p, err = c.Parameter(b.Offset)
		}
		if b.Kind == PrecedingBound {
			return p + " PRECEDING", err
		}
		return p + " FOLLOWING", err
	default:
		return "", fault.New(fault.Invalid, "invalid frame boundary")
	}
}

// Ordinary aggregates inside window arguments/orderings group the outer SELECT;
// the aggregate receiving OVER itself does not. Scalar subqueries own their groups.
func GroupsSelect(v ValueExpression) bool {
	return GroupsValue(v, 0)
}

func GroupsValue(v ValueExpression, depth int) bool {
	if depth > MaxExpressionDepth {
		return false
	}
	groups := false
	if handled, _ := VisitComputedValue(v, func(v ValueExpression) error { groups = groups || GroupsValue(v, depth+1); return nil }, func(p Expression) error { groups = groups || GroupsCondition(p, depth+1); return nil }); handled {
		return groups
	}
	switch v := v.(type) {
	case AggregateNode:
		return true
	case WindowNode:
		window, err := ResolveWindow(v.Window, false, 0)
		if err != nil {
			return false
		}
		for _, key := range window.Partitions {
			if GroupsValue(key, depth+1) {
				return true
			}
		}
		if GroupsValue(v.Input, depth+1) {
			return true
		}
		for _, o := range window.Orders {
			if GroupsValue(o.Expression, depth+1) {
				return true
			}
		}
	}
	return false
}

func GroupsCondition(p Expression, depth int) bool {
	if depth > MaxExpressionDepth {
		return false
	}
	switch p := p.(type) {
	case Comparison:
		return GroupsValue(p.Operand, depth+1)
	case BinaryComparison:
		return GroupsValue(p.Left, depth+1) || GroupsValue(p.Right, depth+1)
	case RowComparison:
		for _, operand := range p.Operands {
			if GroupsValue(operand, depth+1) {
				return true
			}
		}
		return false
	case ScopeNode:
		return false
	case Junction:
		if len(p.Children) > MaxExpressionNodes {
			return false
		}
		for _, child := range p.Children {
			if GroupsCondition(child, depth+1) {
				return true
			}
		}
	case Negation:
		return GroupsCondition(p.Child, depth+1)
	}
	return false
}
