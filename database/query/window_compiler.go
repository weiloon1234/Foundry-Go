package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func (c *compiler) validateWindow(n windowNode, grouped map[fieldRef]bool, grouping bool) error {
	if c.inWindow {
		return fault.New(fault.Invalid, "window functions cannot nest at the same SELECT level")
	}
	if n.err != nil {
		return n.err
	}
	if n.window.err != nil {
		return n.window.err
	}
	resolved, err := resolveWindow(n.window, false, 0)
	if err != nil {
		return err
	}
	n.window = resolved
	if len(n.window.partitions) > MaxExpressionNodes || len(n.window.orders) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "window exceeds its resource bound")
	}
	previous := c.inWindow
	c.inWindow = true
	defer func() { c.inWindow = previous }()
	check := func(v valueExpression) error { return c.validateSelectedExpression(v, grouped, grouping) }
	if n.kind != aggregateWindow && n.aggregate != (aggregateNode{}) {
		return fault.New(fault.Invalid, "window function has an unexpected aggregate")
	}
	needsValue := n.kind >= lagWindow && n.kind <= nthValueWindow
	if needsValue {
		if err := check(n.input); err != nil {
			return err
		}
	} else if n.input != nil {
		return fault.New(fault.Invalid, "window function has an unexpected value")
	}
	switch n.kind {
	case rowNumberWindow, rankWindow, denseRankWindow, percentRankWindow, cumeDistWindow, firstValueWindow, lastValueWindow:
		if n.n != 0 {
			return fault.New(fault.Invalid, "window function has an unexpected position")
		}
	case ntileWindow, nthValueWindow:
		if n.n <= 0 {
			return fault.New(fault.Invalid, "window position or bucket count must be positive")
		}
	case lagWindow, leadWindow:
		// PostgreSQL permits negative offsets (the opposite direction).
	case aggregateWindow:
		if n.n != 0 || n.aggregate.kind == countDistinct {
			return fault.New(fault.Invalid, "unsupported aggregate window declaration")
		}
		if err := n.aggregate.validate(func(f fieldRef) error { return check(f) }); err != nil {
			return err
		}
		if err := n.aggregate.validateFilter(func(f fieldRef) error { return check(f) }, &c.expressionNodes); err != nil {
			return err
		}
	default:
		return fault.New(fault.Invalid, "invalid window function")
	}
	if (n.hasFallback || n.fallback != nil) && n.kind != lagWindow && n.kind != leadWindow {
		return fault.New(fault.Invalid, "only lag/lead accept fallback values")
	}
	if !n.hasFallback && n.fallback != nil {
		return fault.New(fault.Invalid, "unexpected window fallback")
	}
	var seen valueIndex
	for _, key := range n.window.partitions {
		if err := check(key); err != nil {
			return err
		}
		plan, err := c.planValue(key)
		if err != nil {
			return err
		}
		_, found, err := seen.find(plan)
		if err != nil {
			return err
		}
		if found {
			return fault.New(fault.Invalid, "repeated window partition key")
		}
		seen.add(plan)
	}
	for _, order := range n.window.orders {
		if err := check(order.expression); err != nil {
			return err
		}
	}
	return n.window.validateFrame()
}

func (w windowSpec) validateFrame() error {
	f := w.frame
	if f == nil {
		return nil
	}
	if f.kind < rowsFrame || f.kind > rangeFrame || f.exclusion > excludeTies {
		return fault.New(fault.Invalid, "invalid window frame kind or exclusion")
	}
	if f.kind == groupsFrame && len(w.orders) == 0 {
		return fault.New(fault.Invalid, "GROUPS frame requires window ordering")
	}
	if f.rangeType != 0 {
		if f.kind != rangeFrame || len(w.orders) != 1 {
			return fault.New(fault.Invalid, "value-distance RANGE requires exactly one ordering expression")
		}
		switch f.rangeType {
		case codec.TypeInteger, codec.TypeFloat, codec.TypeDecimal, codec.TypeDate, codec.TypeTime, codec.TypeDateTime, codec.TypeLocalDateTime:
		default:
			return fault.New(fault.Invalid, "unsupported RANGE ordering representation")
		}
	}
	for _, bound := range []frameBound{f.start, f.end} {
		switch bound.kind {
		case unboundedPreceding, currentRow, unboundedFollowing:
			if bound.offset != 0 || bound.distance != nil {
				return fault.New(fault.Invalid, "frame position has an unexpected offset")
			}
		case precedingBound, followingBound:
			if f.kind == rangeFrame {
				if bound.offset != 0 || bound.distance == nil || f.rangeType == 0 {
					return fault.New(fault.Invalid, "RANGE offset requires a typed distance")
				}
				if err := bound.distance.validate(f.rangeType); err != nil {
					return err
				}
			} else if bound.offset < 0 || bound.distance != nil {
				return fault.New(fault.Invalid, "frame offset requires a nonnegative ROWS/GROUPS distance")
			}
		default:
			return fault.New(fault.Invalid, "invalid window frame boundary")
		}
	}
	if f.start.kind == unboundedFollowing || f.end.kind == unboundedPreceding || f.end.kind < f.start.kind {
		return fault.New(fault.Invalid, "invalid window frame boundary order")
	}
	return nil
}

func (c *compiler) windowSQL(n windowNode, grouped map[fieldRef]bool, grouping bool) (string, error) {
	previous := c.inWindow
	c.inWindow = true
	defer func() { c.inWindow = previous }()
	var call string
	if n.kind == aggregateWindow {
		a := n.aggregate
		if a.kind == existsValues {
			a.kind = countAll
		}
		var err error
		call, err = c.aggregateAt(a, func(f fieldRef) error { return c.validateSelectedExpression(f, grouped, grouping) }, grouped, grouping, true)
		if err != nil {
			return "", err
		}
	} else {
		names := [...]string{"", "ROW_NUMBER", "RANK", "DENSE_RANK", "PERCENT_RANK", "CUME_DIST", "NTILE", "LAG", "LEAD", "FIRST_VALUE", "LAST_VALUE", "NTH_VALUE"}
		if n.kind < rowNumberWindow || int(n.kind) >= len(names) {
			return "", fault.New(fault.Invalid, "invalid window function")
		}
		var args []string
		if n.input != nil {
			text, err := c.selectedExpression(n.input, grouped, grouping)
			if err != nil {
				return "", err
			}
			args = append(args, text)
		}
		if n.kind == ntileWindow || n.kind == lagWindow || n.kind == leadWindow || n.kind == nthValueWindow {
			p, err := c.parameter(int64(n.n))
			if err != nil {
				return "", err
			}
			args = append(args, p)
		}
		if n.hasFallback {
			p, err := c.parameter(n.fallback)
			if err != nil {
				return "", err
			}
			args = append(args, p)
		}
		call = names[n.kind] + "(" + strings.Join(args, ", ") + ")"
	}
	spec, err := c.windowSpecSQL(n.window, grouped, grouping)
	if err != nil {
		return "", err
	}
	result := call + " OVER (" + spec + ")"
	if n.window.referenceOnly() {
		result = call + " OVER " + quoted(n.window.reference.name)
	}
	if n.kind == aggregateWindow && n.aggregate.kind == existsValues {
		result = "(" + result + " > 0)"
	}
	return result, nil
}

func (c *compiler) windowSpecSQL(w windowSpec, grouped map[fieldRef]bool, grouping bool) (string, error) {
	var parts []string
	if w.reference != nil {
		parts = append(parts, quoted(w.reference.name))
	}
	if len(w.partitions) != 0 {
		names := make([]string, len(w.partitions))
		for i, f := range w.partitions {
			text, err := c.selectedExpression(f, grouped, grouping)
			if err != nil {
				return "", err
			}
			names[i] = text
		}
		parts = append(parts, "PARTITION BY "+strings.Join(names, ", "))
	}
	if len(w.orders) != 0 {
		names := make([]string, len(w.orders))
		for i, order := range w.orders {
			text, err := c.selectedExpression(order.expression, grouped, grouping)
			if err != nil {
				return "", err
			}
			names[i] = text + orderDirection(order)
		}
		parts = append(parts, "ORDER BY "+strings.Join(names, ", "))
	}
	if f := w.frame; f != nil {
		start, err := c.frameBoundSQL(f.start)
		if err != nil {
			return "", err
		}
		end, err := c.frameBoundSQL(f.end)
		if err != nil {
			return "", err
		}
		name := [...]string{"", "ROWS", "GROUPS", "RANGE"}[f.kind]
		text := name + " BETWEEN " + start + " AND " + end
		text += [...]string{"", " EXCLUDE CURRENT ROW", " EXCLUDE GROUP", " EXCLUDE TIES"}[f.exclusion]
		parts = append(parts, text)
	}
	return strings.Join(parts, " "), nil
}

func (c *compiler) frameBoundSQL(b frameBound) (string, error) {
	switch b.kind {
	case unboundedPreceding:
		return "UNBOUNDED PRECEDING", nil
	case currentRow:
		return "CURRENT ROW", nil
	case unboundedFollowing:
		return "UNBOUNDED FOLLOWING", nil
	case precedingBound, followingBound:
		var p string
		var err error
		if b.distance != nil {
			p, err = c.rangeDistanceSQL(*b.distance)
		} else {
			p, err = c.parameter(b.offset)
		}
		if b.kind == precedingBound {
			return p + " PRECEDING", err
		}
		return p + " FOLLOWING", err
	default:
		return "", fault.New(fault.Invalid, "invalid frame boundary")
	}
}

// Ordinary aggregates inside window arguments/orderings group the outer SELECT;
// the aggregate receiving OVER itself does not. Scalar subqueries own their groups.
func groupsSelect(v valueExpression) bool {
	return groupsValue(v, 0)
}
func groupsValue(v valueExpression, depth int) bool {
	if depth > MaxExpressionDepth {
		return false
	}
	groups := false
	if handled, _ := visitComputedValue(v, func(v valueExpression) error { groups = groups || groupsValue(v, depth+1); return nil }, func(p expression) error { groups = groups || groupsCondition(p, depth+1); return nil }); handled {
		return groups
	}
	switch v := v.(type) {
	case aggregateNode:
		return true
	case windowNode:
		window, err := resolveWindow(v.window, false, 0)
		if err != nil {
			return false
		}
		for _, key := range window.partitions {
			if groupsValue(key, depth+1) {
				return true
			}
		}
		if groupsValue(v.input, depth+1) {
			return true
		}
		for _, o := range window.orders {
			if groupsValue(o.expression, depth+1) {
				return true
			}
		}
	}
	return false
}
func groupsCondition(p expression, depth int) bool {
	if depth > MaxExpressionDepth {
		return false
	}
	switch p := p.(type) {
	case comparison:
		return groupsValue(p.operand, depth+1)
	case binaryComparison:
		return groupsValue(p.left, depth+1) || groupsValue(p.right, depth+1)
	case rowComparison:
		for _, operand := range p.operands {
			if groupsValue(operand, depth+1) {
				return true
			}
		}
		return false
	case scopeNode:
		return false
	case junction:
		if len(p.children) > MaxExpressionNodes {
			return false
		}
		for _, child := range p.children {
			if groupsCondition(child, depth+1) {
				return true
			}
		}
	case negation:
		return groupsCondition(p.child, depth+1)
	}
	return false
}
