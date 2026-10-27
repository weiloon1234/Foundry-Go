package querycore

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// AnnotatedValueAlias is the computed column paired with a complete model.
const AnnotatedValueAlias = "foundry_value"

func SelectRecordNode(node SelectNode, table string, columns []Column, hasDecoder bool) (SelectNode, error) {
	if table == "" || !hasDecoder || len(columns) == 0 || len(columns) > MaxExpressionNodes || len(node.Joins) > MaxExpressionNodes {
		return SelectNode{}, safeerror.NewFault(fault.Invalid, "record selection requires a bounded, complete source and decoder")
	}
	// Validate the scope against this concrete query as well as its static type.
	// A later RIGHT/FULL join can make an entire preceding join chain nullable.
	found := false
	check := func(source TableSource, nullable bool) error {
		if source.Name() != table {
			return nil
		}
		if found || nullable || !slices.Equal(source.Columns, columns) {
			return safeerror.NewFault(fault.Invalid, "record scope must identify one complete, preserved source")
		}
		found = true
		return nil
	}
	if err := check(node.Source, false); err != nil {
		return SelectNode{}, err
	}
	for _, join := range node.Joins {
		if found && (join.Kind == RightJoin || join.Kind == FullJoin) {
			return SelectNode{}, safeerror.NewFault(fault.Invalid, "record selection cannot hydrate a nullable join side")
		}
		if err := check(join.Source, join.Kind == LeftJoin || join.Kind == FullJoin); err != nil {
			return SelectNode{}, err
		}
	}
	if !found {
		return SelectNode{}, safeerror.NewFault(fault.Invalid, "record scope is not available in this source")
	}
	node.Selections = ColumnSelections(table, columns)
	return node, nil
}
