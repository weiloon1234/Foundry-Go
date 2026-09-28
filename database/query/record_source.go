package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// RecordQuerySource exposes a complete model or declared projection as a typed
// SELECT source. Its record type remains separate from the input query scope.
type RecordQuerySource[R any] interface{ recordQuery() recordQuery[R] }
type recordQuery[R any] struct {
	_            [0]*R
	node         selectNode
	columns      []Column
	scan         func(database.Row) (R, error)
	fields       []RecordField[R]
	lifecycle    *readLifecycle[R]
	directSource bool
	err          error
}

func (q Query[M]) recordQuery() recordQuery[M] {
	r := recordQuery[M]{}
	if err := q.validateCore(); err != nil {
		r.err = err
		return r
	}
	if q.definition == nil || len(q.relations) != 0 || q.relationLimits != nil {
		r.err = fault.New(fault.Invalid, "record sources require model metadata without eager-loading options")
		return r
	}
	r.node, r.columns = q.modelSelect(), q.definition.columns
	r.scan = q.definition.scan
	r.fields = q.definition.modelFields
	r.lifecycle = q.definition.retrieval()
	r.directSource = len(r.node.predicates) == 0 && len(q.orders) == 0 && !q.limit.IsSet() && q.offset == 0
	return r
}
func (q ProjectionQuery[S, P]) recordQuery() recordQuery[P] {
	node, err := q.selectNode()
	metadata := q.recordMetadata()
	return recordQuery[P]{node: node, columns: metadata.columns, scan: metadata.scan, fields: metadata.fields, lifecycle: metadata.lifecycle, err: err}
}

func (q recordQuery[R]) metadata() *recordMetadata[R] {
	return &recordMetadata[R]{columns: q.columns, scan: q.scan, fields: q.fields, lifecycle: q.lifecycle}
}
