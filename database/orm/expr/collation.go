package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Collation is an unqualified PostgreSQL collation name, resolved through search_path.
// Names are validated at query build and rendered as one double-quoted identifier.
//
// Example: expr.Asc(expr.Collate(name, expr.ICUCollation("zh")))
type Collation string

// Validate checks a non-empty collation name without checking database availability.
func (c Collation) Validate() error { return querycore.ValidateCollationName(string(c)) }

const (
	// CollationC sorts text in byte order and is always available in PostgreSQL.
	CollationC Collation = "C"
	// CollationUnicode uses language-neutral Unicode ordering on ICU-enabled PostgreSQL.
	CollationUnicode Collation = "und-x-icu"
	invalidCollation Collation = "\x00"
)

// ICUCollation names a predefined ICU collation imported by PostgreSQL at initdb.
// Tags may contain only ASCII letters, digits and hyphens. An invalid tag returns
// an invalid Collation, which fails through the ordinary validation error path.
//
// Example: expr.ICUCollation("en-US")
func ICUCollation(languageTag string) Collation {
	if languageTag == "" {
		return invalidCollation
	}
	for _, c := range languageTag {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return invalidCollation
		}
	}
	return Collation(languageTag + "-x-icu")
}

// Collate chooses a text row value's collation without changing its value type or codec.
//
// Example: expr.Asc(expr.Collate(name, expr.CollationUnicode))
func Collate[S any, V ~string](input RowValue[S, V], collation Collation) TextRowExpression[S, V] {
	return TextRowExpression[S, V]{data: CollateValue(AsSelected(rowInput(input)), collation).data}
}

// CollateValue applies Collate to selected expressions, including aggregates/windows.
//
// Example: expr.CollateValue(input, expr.ICUCollation("en-US"))
func CollateValue[S any, V ~string](input Selected[S, V], collation Collation) Selected[S, V] {
	return Selected[S, V]{data: collateData(checkedTextColumn(input.data), collation, false)}
}

// CollateNullable chooses a nullable text row value's collation, preserving NULL and its codec.
//
// Example: expr.CollateNullable(note, expr.CollationUnicode)
func CollateNullable[S any, V ~string](input RowValue[S, value.Nullable[V]], collation Collation) NullableTextRowExpression[S, V] {
	return NullableTextRowExpression[S, V]{data: CollateNullableValue(AsSelected(rowInput(input)), collation).data}
}

// CollateNullableValue applies CollateNullable to selected expressions, including aggregates/windows.
//
// Example: expr.CollateNullableValue(input, expr.CollationC)
func CollateNullableValue[S any, V ~string](input Selected[S, value.Nullable[V]], collation Collation) Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: collateData(checkedTextColumn(input.data), collation, false)}
}

func collateData[V any](input rowData[V], collation Collation, defaultCollation bool) rowData[V] {
	if !input.column.Collatable {
		input.core.Err = safeerror.Join("operation failed", input.core.Err, ormcore.Invalid("collation requires a collatable expression"))
	}
	input.core.Node = querycore.OperationNode{
		Kind: querycore.CollateOperation, Result: input.column.Kind, Collation: string(collation),
		DefaultCollation: defaultCollation,
		Arguments:        []querycore.OperationArgument{{Value: input.core.Node, Kind: input.column.Kind}},
	}
	return input
}

// FoundryDefaultCollation applies a framework sort default using SQL type metadata.
// Explicit collation on the value or its text operands takes precedence.
//
// Example: expr.FoundryDefaultCollation(selected, collation, seal)
func FoundryDefaultCollation[S, V any](input Selected[S, V], collation Collation, seal sqlowner.ExpressionSeal) Selected[S, V] {
	if !seal.Valid() {
		input.data.core.Err = ormcore.Invalid("invalid SQL owner capability")
		return input
	}
	if collation == "" || !input.data.column.Collatable {
		return input
	}
	explicit, err := explicitCollation(input.data.core.Node)
	input.data.core.Err = safeerror.Join("operation failed", input.data.core.Err, err)
	if !explicit && err == nil {
		input.data = collateData(input.data, collation, true)
	}
	return input
}

func explicitCollation(input querycore.ValueExpression) (bool, error) {
	var bounds querycore.SelectWalk
	found := false
	var visit func(querycore.ValueExpression, int) error
	visit = func(node querycore.ValueExpression, depth int) error {
		if found || !bounds.Visit(depth) {
			return bounds.Err
		}
		if operation, ok := node.(querycore.OperationNode); ok {
			if operation.Kind == querycore.CollateOperation {
				found = true
				return nil
			}
			if operation.Result != codec.TypeText {
				return nil
			}
		}
		if handled, err := querycore.VisitComputedValue(node, func(child querycore.ValueExpression) error {
			return visit(child, depth+1)
		}, func(querycore.Expression) error { return nil }); handled {
			return err
		}
		switch node := node.(type) {
		case querycore.WindowNode:
			if node.Input != nil {
				return visit(node.Input, depth+1)
			}
		case querycore.ScalarSubquery:
			if len(node.Query.Node.Selections) == 1 {
				return visit(node.Query.Node.Selections[0].Expression, depth+1)
			}
		}
		return nil
	}
	err := visit(input, 0)
	return found, err
}
