package query

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Private metadata carries the original ordered decoder through scope changes.
// No field mapping is inferred and no new model/schema declaration is created.
type recordMetadata[R any] struct {
	columns   []columnData
	scan      func(database.Row) (R, error)
	fields    []RecordField[R]
	lifecycle *readLifecycle[R]
}

type recordSelection[R any] struct {
	table    string
	metadata *recordMetadata[R]
}

// SelectRecord selects every persisted model field or every declared projection
// field from a preserved source scope. The result retains the input's filters,
// joins and window, and uses the record's original decoder. It has no model
// writes or implicit relation loading. Nullable outer-join sides require a
// declared projection instead. Obtain scope from Query.Scope, an alias or set,
// adapting it with LeftScope/RightScope when selecting from a join.
func SelectRecord[S, R any](source ProjectionSource[S], scope RecordScope[S, R]) ProjectionQuery[S, R] {
	q := ProjectionQuery[S, R]{record: &recordSelection[R]{table: scope.table, metadata: scope.record}}
	if nilDescriptor(source) {
		q.source.err = fault.New(fault.Invalid, "record selection requires a source")
		return q
	}
	q.source = source.projectionSource()
	return q
}

func (r recordSelection[R]) selectNode(node selectNode) (selectNode, error) {
	if r.metadata == nil {
		return selectRecordNode(node, r.table, nil, false)
	}
	return selectRecordNode(node, r.table, r.metadata.columns, r.metadata.scan != nil)
}
