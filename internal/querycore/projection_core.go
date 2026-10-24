package querycore

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
)

// Projection plans erase scope identities only after Map/Project checked the
// input, output and value types. Decoder/getter values stay concretely typed.
type ProjectionColumn struct {
	Sensitive bool
	Name      string
	Typ       reflect.Type
	Nullable  bool
}

type ProjectionMapping struct {
	Sensitive  bool
	Column     ProjectionColumn
	Expression ValueExpression
}

func ValidateProjectionColumns(columns []ProjectionColumn, hasDecoder bool) error {
	if len(columns) == 0 || len(columns) > MaxExpressionNodes || !hasDecoder {
		return fault.New(fault.Invalid, "projection requires bounded columns and a complete decoder")
	}
	seen := make(map[string]bool, len(columns))
	for _, column := range columns {
		if !sqlname.Valid(column.Name) || seen[column.Name] || column.Typ == nil {
			return fault.New(fault.Invalid, "invalid or repeated projection field")
		}
		seen[column.Name] = true
	}

	return nil
}

func SelectProjection(source SelectNode, columns []ProjectionColumn, mappings []ProjectionMapping) (SelectNode, error) {
	if len(mappings) != len(columns) {
		return SelectNode{}, fault.New(fault.Invalid, "projection must select every declared result field exactly once")
	}
	byName := make(map[string]ProjectionMapping, len(mappings))
	for _, m := range mappings {
		if _, exists := byName[m.Column.Name]; exists {
			return SelectNode{}, fault.New(fault.Invalid, "repeated projection field mapping")
		}
		byName[m.Column.Name] = m
	}
	node := source
	node.Selections = make([]SelectItem, len(columns))
	for i, column := range columns {
		mapping, exists := byName[column.Name]
		if !exists || mapping.Column.Typ != column.Typ {
			return SelectNode{}, fault.New(fault.Invalid, "projection mapping does not match its declared field")
		}
		node.Selections[i] = SelectItem{Expression: mapping.Expression, Alias: column.Name, Sensitive: column.Sensitive || mapping.Sensitive}
	}
	return node, nil
}
