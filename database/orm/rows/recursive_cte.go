package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// RecursiveSelf exposes the working record only through a typed alias.
// Captured references fail compilation outside their owning recursive step.
// Example: rows.AsRecord[parent](self,"parent").
type RecursiveSelf[R any] struct {
	_             [0]*R
	recursiveSelf [0]struct{}
	core          ormcore.RecordSource
}

func (s RecursiveSelf[R]) recordData(*R) ormcore.RecordSource { return s.core }

// RecursiveCTE uses UNION to remove duplicate complete records across iterations.
// The step runs once synchronously to build SQL and must reference self exactly once.
// Example: rows.RecursiveCTE("tree",anchor,func(self rows.RecursiveSelf[Node]) rows.RecordSource[Node] { return step(self) }).
func RecursiveCTE[R any](name string, anchor RecordSource[R], step func(RecursiveSelf[R]) RecordSource[R]) CommonTable[R] {
	return recursiveTable(name, anchor, step, querycore.UnionSet)
}

// RecursiveAllCTE retains duplicates and requires application data to terminate recursion.
// Execute with a cancellable context; an outer LIMIT is not a recursion bound.
// Example: rows.RecursiveAllCTE("paths",anchor,step).
func RecursiveAllCTE[R any](name string, anchor RecordSource[R], step func(RecursiveSelf[R]) RecordSource[R]) CommonTable[R] {
	return recursiveTable(name, anchor, step, querycore.UnionAllSet)
}

func recursiveTable[R any](name string, anchor RecordSource[R], step func(RecursiveSelf[R]) RecordSource[R], operator querycore.SetOperator) CommonTable[R] {
	c := CTE(name, anchor)
	if c.core.Record.Source.Err != nil {
		return c
	}
	if step == nil {
		c.core.Record.Source.Err = ormcore.Invalid("recursive CTE requires a step")
		return c
	}
	self, ref := ormcore.RecursiveRecord(name, c.core.Record)
	result := recordOf(step(RecursiveSelf[R]{core: self}))
	c.core = ormcore.CompleteRecursion(c.core, result, ref, operator)
	return c
}
