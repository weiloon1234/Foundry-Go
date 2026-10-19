package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type recursiveReference = querycore.RecursiveReference

type recursiveCTE = querycore.RecursiveCTE

// RecursiveSelf is the current working table inside a recursive CTE's step.
// Use As to obtain a typed scope. A reference captured outside its owning step
// fails query compilation; it has no standalone execution or mutation methods.
type RecursiveSelf[R any] struct {
	_         [0]*R
	reference *recursiveReference
	scan      func(database.Row) (R, error)
	fields    []RecordField[R]
	lifecycle *readLifecycle[R]
}

func (s RecursiveSelf[R]) recordQuery() recordQuery[R] {
	if s.reference == nil || s.scan == nil {
		return recordQuery[R]{err: fault.New(fault.Invalid, "recursive self requires an owning CTE")}
	}
	r := s.reference
	return recordQuery[R]{node: selectNode{
		Source: tableSource{Self: r, Columns: r.Columns}, Selections: columnSelections(r.Name, r.Columns),
	}, columns: r.Columns, scan: s.scan, fields: s.fields, lifecycle: s.lifecycle, directSource: true}
}

// RecursiveCTE combines an anchor with a recursive step using UNION. Duplicate
// complete output rows are discarded across iterations. This can terminate
// cycles over unchanged records; it is not a general cycle/depth bound.
// The callback builds the step once, synchronously, and performs no database I/O
// on behalf of the framework. Its result must retain the anchor's record type.
func RecursiveCTE[R any](name string, anchor RecordQuerySource[R], step func(RecursiveSelf[R]) RecordQuerySource[R]) CommonTable[R] {
	return recursiveTable(name, anchor, step, unionSet)
}

// RecursiveAllCTE uses UNION ALL, retaining duplicate rows and traversal paths.
// The step must terminate for the application's data; use a cancellable context
// with a deadline when executing. An outer LIMIT is not a recursion safety bound.
func RecursiveAllCTE[R any](name string, anchor RecordQuerySource[R], step func(RecursiveSelf[R]) RecordQuerySource[R]) CommonTable[R] {
	return recursiveTable(name, anchor, step, unionAllSet)
}

func recursiveTable[R any](name string, anchor RecordQuerySource[R], step func(RecursiveSelf[R]) RecordQuerySource[R], operator setOperator) CommonTable[R] {
	if !sqlname.Valid(name) || nilDescriptor(anchor) || step == nil {
		return CommonTable[R]{err: fault.New(fault.Invalid, "recursive CTE requires a valid name, anchor and step")}
	}
	a := anchor.recordQuery()
	if a.err != nil {
		return CommonTable[R]{err: a.err}
	}
	reference := &recursiveReference{Name: name, Columns: a.columns}
	result := step(RecursiveSelf[R]{reference: reference, scan: a.scan, fields: a.fields, lifecycle: a.lifecycle})
	if nilDescriptor(result) {
		return CommonTable[R]{err: fault.New(fault.Invalid, "recursive CTE step requires a record query")}
	}
	b := result.recordQuery()
	if err := compatibleRecords(a, b); err != nil {
		return CommonTable[R]{err: err}
	}
	return CommonTable[R]{definition: &cteNode{
		Name: name, Query: a.node, Columns: a.columns,
		Recursion: &recursiveCTE{Reference: reference, Step: b.node, Operator: operator},
	}, scan: a.scan, fields: a.fields, lifecycle: a.lifecycle}
}
