package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Private metadata carries the original ordered decoder through scope changes.
// No field mapping is inferred and no new model/schema declaration is created.
type recordMetadata[R any] struct {
	columns   []Column
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
	if r.table == "" || r.metadata == nil || r.metadata.scan == nil || len(r.metadata.columns) == 0 || len(r.metadata.columns) > MaxExpressionNodes || len(node.joins) > MaxExpressionNodes {
		return selectNode{}, fault.New(fault.Invalid, "record selection requires a bounded, complete source and decoder")
	}
	// Validate the scope against this concrete query as well as its static type.
	// A later RIGHT/FULL join can make an entire preceding join chain nullable.
	found := false
	check := func(source tableSource, nullable bool) error {
		if source.name() != r.table {
			return nil
		}
		if found || nullable || !slices.Equal(source.columns, r.metadata.columns) {
			return fault.New(fault.Invalid, "record scope must identify one complete, preserved source")
		}
		found = true
		return nil
	}
	if err := check(node.source, false); err != nil {
		return selectNode{}, err
	}
	for _, join := range node.joins {
		if found && (join.kind == rightJoin || join.kind == fullJoin) {
			return selectNode{}, fault.New(fault.Invalid, "record selection cannot hydrate a nullable join side")
		}
		if err := check(join.source, join.kind == leftJoin || join.kind == fullJoin); err != nil {
			return selectNode{}, err
		}
	}
	if !found {
		return selectNode{}, fault.New(fault.Invalid, "record scope is not available in this source")
	}
	node.selections = columnSelections(r.table, r.metadata.columns)
	return node, nil
}
