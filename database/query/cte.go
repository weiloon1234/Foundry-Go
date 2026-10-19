package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

type cteMaterialization = querycore.CteMaterialization

const cteDefault = querycore.CteDefault

const cteMaterialized = querycore.CteMaterialized

const cteNotMaterialized = querycore.CteNotMaterialized

type cteNode = querycore.CteNode

// CommonTable declares a reusable, read-only CTE with a complete record type.
// Use As to give each reference a typed scope for fields, joins and projections.
type CommonTable[R any] struct {
	_          [0]*R
	definition *cteNode
	scan       func(database.Row) (R, error)
	fields     []RecordField[R]
	lifecycle  *readLifecycle[R]
	err        error
}

// CTE names a model or complete projection query for use within one statement.
// References carry their definition; Foundry emits dependencies before their
// consumers and emits each reused definition once. It performs no database I/O.
func CTE[R any](name string, source RecordQuerySource[R]) CommonTable[R] {
	if !sqlname.Valid(name) || nilDescriptor(source) {
		return CommonTable[R]{err: fault.New(fault.Invalid, "CTE requires a valid name and record query")}
	}
	q := source.recordQuery()
	return CommonTable[R]{definition: &cteNode{Name: name, Query: q.node, Columns: q.columns}, scan: q.scan, fields: q.fields, lifecycle: q.lifecycle, err: q.err}
}

// Materialized requests PostgreSQL's explicit materialization behavior.
func (c CommonTable[R]) Materialized() CommonTable[R] { return c.withMaterialization(cteMaterialized) }

// NotMaterialized permits PostgreSQL to inline this read-only CTE. It may cause
// repeated evaluation when several references use the definition.
// Recursive CTEs cannot be inlined; requesting this mode on one fails compilation.
func (c CommonTable[R]) NotMaterialized() CommonTable[R] {
	return c.withMaterialization(cteNotMaterialized)
}

func (c CommonTable[R]) withMaterialization(mode cteMaterialization) CommonTable[R] {
	if c.definition != nil && c.definition.Materialization != mode {
		definition := *c.definition
		definition.Materialization = mode
		c.definition = &definition
	}
	return c
}

func (c CommonTable[R]) recordQuery() recordQuery[R] {
	if c.err != nil {
		return recordQuery[R]{err: c.err}
	}
	if c.definition == nil {
		return recordQuery[R]{err: fault.New(fault.Invalid, "CTE requires a definition")}
	}
	d := c.definition
	return recordQuery[R]{node: selectNode{
		Source: tableSource{Cte: d, Columns: d.Columns}, Selections: columnSelections(d.Name, d.Columns),
	}, columns: d.Columns, scan: c.scan, fields: c.fields, lifecycle: c.lifecycle, directSource: true}
}
