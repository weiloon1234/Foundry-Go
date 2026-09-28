package query

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type namedWindow struct {
	name       string
	definition windowSpec
}

// Named declares a reusable window in this query scope. References carry their
// definition; each used definition is emitted once in the SELECT's WINDOW
// clause. Declare once and reuse the returned value to share an SQL name.
// A direct reference may have a frame. Deriving ordering/frame clauses from
// it follows PostgreSQL inheritance: the referenced definition must be frameless.
func (w Window[S]) Named(name string) Window[S] {
	if !sqlname.Valid(name) {
		w.node.err = fault.New(fault.Invalid, "named window requires a valid SQL identifier")
		return w
	}
	return Window[S]{node: windowSpec{reference: &namedWindow{name: name, definition: w.node}}}
}

func (w windowSpec) referenceOnly() bool {
	return w.reference != nil && len(w.partitions) == 0 && len(w.orders) == 0 && w.frame == nil
}

// Resolution validates inheritance once for both window functions and WINDOW
// definitions. Bare OVER name differs from copying a definition in parentheses.
func resolveWindow(w windowSpec, copying bool, depth int) (windowSpec, error) {
	if depth > MaxExpressionDepth {
		return windowSpec{}, fault.New(fault.Invalid, "named window exceeds its depth bound")
	}
	if w.err != nil {
		return windowSpec{}, w.err
	}
	if w.reference == nil {
		return w, nil
	}
	if !sqlname.Valid(w.reference.name) {
		return windowSpec{}, fault.New(fault.Invalid, "invalid named window reference")
	}
	base, err := resolveWindow(w.reference.definition, true, depth+1)
	if err != nil {
		return windowSpec{}, err
	}
	if !copying && w.referenceOnly() {
		return base, nil
	}
	if base.frame != nil {
		return windowSpec{}, fault.New(fault.Invalid, "a framed named window cannot be extended or inherited")
	}
	if len(w.partitions) != 0 {
		return windowSpec{}, fault.New(fault.Invalid, "an inherited window cannot add PARTITION BY")
	}
	if len(base.orders) != 0 && len(w.orders) != 0 {
		return windowSpec{}, fault.New(fault.Invalid, "an inherited window cannot replace existing ORDER BY")
	}
	w.partitions = base.partitions
	if len(w.orders) == 0 {
		w.orders = base.orders
	}
	w.reference = nil
	return w, nil
}

// This shared structural boundary lets dependency analysis and local window
// registration traverse definitions without duplicating expression handling.
func visitWindowSpec(w windowSpec, item func(valueExpression) error, reference func(*namedWindow) error) error {
	if w.err != nil {
		return w.err
	}
	if len(w.partitions) > MaxExpressionNodes || len(w.orders) > MaxExpressionNodes {
		return fault.New(fault.Invalid, "window exceeds its resource bound")
	}
	if w.reference != nil {
		if err := reference(w.reference); err != nil {
			return err
		}
	}
	for _, v := range w.partitions {
		if err := item(v); err != nil {
			return err
		}
	}
	for _, o := range w.orders {
		if err := item(o.expression); err != nil {
			return err
		}
	}
	return nil
}

func orderedWindows(s selectNode) ([]*namedWindow, error) {
	names := make(map[string]*namedWindow)
	active, visited := make(map[*namedWindow]bool), make(map[*namedWindow]bool)
	var order []*namedWindow
	walk := selectWalk{localSelect: true}
	walk.named = func(d *namedWindow, depth int) {
		if !sqlname.Valid(d.name) {
			walk.err = fault.New(fault.Invalid, "invalid named window definition")
			return
		}
		if other := names[d.name]; other != nil && other != d {
			walk.err = fault.New(fault.Invalid, "SELECT repeats a window name with different definitions")
			return
		}
		if active[d] {
			walk.err = fault.New(fault.Invalid, "cyclic named window dependency")
			return
		}
		if visited[d] {
			return
		}
		names[d.name], active[d] = d, true
		walk.windowSpec(d.definition, depth+1)
		active[d], visited[d] = false, true
		order = append(order, d)
	}
	walk.selectNode(s, 0)
	return order, walk.err
}

func (c *compiler) namedWindowsSQL(definitions []*namedWindow, grouped map[fieldRef]bool, grouping bool) (string, error) {
	if len(definitions) == 0 {
		return "", nil
	}
	parts := make([]string, len(definitions))
	for i, d := range definitions {
		resolved, err := resolveWindow(d.definition, true, 0)
		if err != nil {
			return "", err
		}
		if err := c.validateWindow(windowNode{kind: rowNumberWindow, window: resolved}, grouped, grouping); err != nil {
			return "", err
		}
		text, err := c.windowDefinitionSQL(d.definition, grouped, grouping)
		if err != nil {
			return "", err
		}
		parts[i] = quoted(d.name) + " AS (" + text + ")"
	}
	return " WINDOW " + strings.Join(parts, ", "), nil
}

func (c *compiler) windowDefinitionSQL(w windowSpec, grouped map[fieldRef]bool, grouping bool) (string, error) {
	previous := c.inWindow
	c.inWindow = true
	defer func() { c.inWindow = previous }()
	return c.windowSpecSQL(w, grouped, grouping)
}
