package querycore

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type NamedWindow struct {
	Name       string
	Definition WindowSpec
}

func (w WindowSpec) ReferenceOnly() bool {
	return w.Reference != nil && len(w.Partitions) == 0 && len(w.Orders) == 0 && w.Frame == nil
}

// Resolution validates inheritance once for both window functions and WINDOW
// definitions. Bare OVER name differs from copying a definition in parentheses.
func ResolveWindow(w WindowSpec, copying bool, depth int) (WindowSpec, error) {
	if depth > MaxExpressionDepth {
		return WindowSpec{}, safeerror.NewFault(fault.Invalid, "named window exceeds its depth bound")
	}
	if w.Err != nil {
		return WindowSpec{}, w.Err
	}
	if w.Reference == nil {
		return w, nil
	}
	if !sqlname.Valid(w.Reference.Name) {
		return WindowSpec{}, safeerror.NewFault(fault.Invalid, "invalid named window reference")
	}
	base, err := ResolveWindow(w.Reference.Definition, true, depth+1)
	if err != nil {
		return WindowSpec{}, err
	}
	if !copying && w.ReferenceOnly() {
		return base, nil
	}
	if base.Frame != nil {
		return WindowSpec{}, safeerror.NewFault(fault.Invalid, "a framed named window cannot be extended or inherited")
	}
	if len(w.Partitions) != 0 {
		return WindowSpec{}, safeerror.NewFault(fault.Invalid, "an inherited window cannot add PARTITION BY")
	}
	if len(base.Orders) != 0 && len(w.Orders) != 0 {
		return WindowSpec{}, safeerror.NewFault(fault.Invalid, "an inherited window cannot replace existing ORDER BY")
	}
	w.Partitions = base.Partitions
	if len(w.Orders) == 0 {
		w.Orders = base.Orders
	}
	w.Reference = nil
	return w, nil
}

// This shared structural boundary lets dependency analysis and local window
// registration traverse definitions without duplicating expression handling.
func VisitWindowSpec(w WindowSpec, item func(ValueExpression) error, reference func(*NamedWindow) error) error {
	if w.Err != nil {
		return w.Err
	}
	if len(w.Partitions) > MaxExpressionNodes || len(w.Orders) > MaxExpressionNodes {
		return safeerror.NewFault(fault.Invalid, "window exceeds its resource bound")
	}
	if w.Reference != nil {
		if err := reference(w.Reference); err != nil {
			return err
		}
	}
	for _, v := range w.Partitions {
		if err := item(v); err != nil {
			return err
		}
	}
	for _, o := range w.Orders {
		if err := item(o.Expression); err != nil {
			return err
		}
	}
	return nil
}

func OrderedWindows(s SelectNode) ([]*NamedWindow, error) {
	names := make(map[string]*NamedWindow)
	active, visited := make(map[*NamedWindow]bool), make(map[*NamedWindow]bool)
	var order []*NamedWindow
	walk := SelectWalk{LocalSelect: true}
	walk.Named = func(d *NamedWindow, depth int) {
		if !sqlname.Valid(d.Name) {
			walk.Err = safeerror.NewFault(fault.Invalid, "invalid named window definition")
			return
		}
		if other := names[d.Name]; other != nil && other != d {
			walk.Err = safeerror.NewFault(fault.Invalid, "SELECT repeats a window name with different definitions")
			return
		}
		if active[d] {
			walk.Err = safeerror.NewFault(fault.Invalid, "cyclic named window dependency")
			return
		}
		if visited[d] {
			return
		}
		names[d.Name], active[d] = d, true
		walk.WindowSpec(d.Definition, depth+1)
		active[d], visited[d] = false, true
		order = append(order, d)
	}
	walk.SelectNode(s, 0)
	return order, walk.Err
}

func (c *Compiler) NamedWindowsSQL(definitions []*NamedWindow, grouped map[FieldRef]bool, grouping bool) (string, error) {
	if len(definitions) == 0 {
		return "", nil
	}
	parts := make([]string, len(definitions))
	for i, d := range definitions {
		resolved, err := ResolveWindow(d.Definition, true, 0)
		if err != nil {
			return "", err
		}
		if err := c.ValidateWindow(WindowNode{Kind: RowNumberWindow, Window: resolved}, grouped, grouping); err != nil {
			return "", err
		}
		text, err := c.WindowDefinitionSQL(d.Definition, grouped, grouping)
		if err != nil {
			return "", err
		}
		parts[i] = Quoted(d.Name) + " AS (" + text + ")"
	}
	return " WINDOW " + strings.Join(parts, ", "), nil
}

func (c *Compiler) WindowDefinitionSQL(w WindowSpec, grouped map[FieldRef]bool, grouping bool) (string, error) {
	previous := c.InWindow
	c.InWindow = true
	defer func() { c.InWindow = previous }()
	return c.WindowSpecSQL(w, grouped, grouping)
}
