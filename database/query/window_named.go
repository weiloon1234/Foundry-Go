package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type namedWindow = querycore.NamedWindow

func resolveWindow(w windowSpec, copying bool, depth int) (windowSpec, error) {
	return querycore.ResolveWindow(w, copying, depth)
}

func visitWindowSpec(w windowSpec, item func(valueExpression) error, reference func(*namedWindow) error) error {
	return querycore.VisitWindowSpec(w, item, reference)
}

func orderedWindows(s selectNode) ([]*namedWindow, error) { return querycore.OrderedWindows(s) }

// Named declares a reusable window in this query scope. References carry their
// definition; each used definition is emitted once in the SELECT's WINDOW
// clause. Declare once and reuse the returned value to share an SQL name.
// A direct reference may have a frame. Deriving ordering/frame clauses from
// it follows PostgreSQL inheritance: the referenced definition must be frameless.
func (w Window[S]) Named(name string) Window[S] {
	if !sqlname.Valid(name) {
		w.node.Err = safeerror.NewFault(fault.Invalid, "named window requires a valid SQL identifier")
		return w
	}
	return Window[S]{node: windowSpec{Reference: &namedWindow{Name: name, Definition: w.node}}}
}
