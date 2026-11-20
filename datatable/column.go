package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Cell preserves the exact DTO field type through export formatting. Ordinary
// scalar cells reuse transport codecs; nullable cells render SQL NULL as empty.
// The table's JSON graph remains authoritative for public field metadata.
// XLSX writes scalar integer, float, decimal, boolean, date and date-time
// cells as typed spreadsheet values when the codec text is exactly
// representable.
//
// Example: datatable.ScalarCell(foundryhttp.StringQuery[string]())
type Cell[V any] struct {
	scalar   foundryhttp.URLScalarInfo
	nullable bool
	kind     cellKind
	format   func(context.Context, V, Presentation) (string, error)
	err      error
}

// ScalarCell formats an exact scalar field with its declared transport codec.
//
// Example: datatable.ScalarCell(codec)
func ScalarCell[V any](codec foundryhttp.QueryCodec[V]) Cell[V] {
	info, err := foundryhttp.DescribePathCodec(codec)
	return Cell[V]{scalar: info, kind: scalarCellKind(info.Value), err: err, format: func(_ context.Context, v V, _ Presentation) (string, error) { return codec.Format(v) }}
}

// NullableCell formats present scalar values with their codec and SQL NULL as empty text.
//
// Example: datatable.NullableCell(codec)
func NullableCell[V any](codec foundryhttp.QueryCodec[V]) Cell[value.Nullable[V]] {
	base := ScalarCell(codec)
	return Cell[value.Nullable[V]]{scalar: base.scalar, nullable: true, kind: base.kind, err: base.err, format: func(ctx context.Context, v value.Nullable[V], p Presentation) (string, error) {
		item, present := v.Get()
		if !present {
			return "", nil
		}
		return base.format(ctx, item, p)
	}}
}

// ColumnInfo describes one declared column for client inspection and export metadata.
//
// Example: datatable.ColumnInfo{Name: "email", Label: "members.email"}
type ColumnInfo struct {
	Name           string                  `json:"name"`
	Label          i18n.MessageKey         `json:"label"`
	Kind           ColumnKind              `json:"kind"`
	Value          contract.ScalarProperty `json:"value"`
	Sortable       bool                    `json:"sortable"`
	Searchable     bool                    `json:"searchable"`
	SearchOperator Operator                `json:"search_operator,omitempty"`
	Exportable     bool                    `json:"exportable"`
	Filter         *FilterInfo             `json:"filter,omitempty"`
}

type filterDeclaration[S any] struct {
	name  string
	label i18n.MessageKey
	info  FilterInfo
	build func(Operator, []string) (condition[S], error)
	err   error
}
type columnDeclaration[S, R any] struct {
	name       string
	label      i18n.MessageKey
	order      func(Direction, expr.Collation) expr.ProjectionOrder[S]
	filter     *filterDeclaration[S]
	searchable bool
	cellInfo   *FilterInfo
	cell       func(context.Context, *R, Presentation) (exportCell, error)
	hidden     bool
	// bind binds the column's row field into the table's plan and returns the
	// bound struct field index.
	bind func(rows.FoundryPlanBuilder[S, R], *R) int
	// dynamicSlot is the DynamicSlot's identity (its plan slot), nil for a
	// statically selected column.
	dynamicSlot any
	err         error
}

type columnExecution[S, R any] struct {
	order  func(Direction, expr.Collation) expr.ProjectionOrder[S]
	filter func(Operator, []string) (condition[S], error)
	cell   func(context.Context, *R, Presentation) (exportCell, error)
}

// ColumnOption preserves the source scope and DTO field type until registration.
// Construct options with SortBy, FilterBy, Searchable, ExportAs and ScanWith.
//
// Example: datatable.SortBy(expr.AsSelected(name))
type ColumnOption[S, V any] struct {
	kind   optionKind
	order  func(Direction, expr.Collation) expr.ProjectionOrder[S]
	source FilterSource[S, V]
	cell   Cell[V]
	scan   rows.FoundryScan[V]
}

type optionKind uint8

const (
	optionSortBy optionKind = iota + 1
	optionFilterBy
	optionSearchable
	optionExportAs
	optionScanWith
)

// SortBy binds a selected value of the same scope and DTO field type.
//
// Example: datatable.Column(w.Username, "users.fields.username", expr.AsSelected(name), datatable.SortBy(expr.AsSelected(name)))
func SortBy[S, V any](expression expr.Selected[S, V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionSortBy, order: makeOrderFunc(expression)}
}

// FilterBy binds a filter source for the column.
//
// Example: datatable.Column(w.Username, "users.fields.username", expr.AsSelected(name), datatable.FilterBy(datatable.Where(text, expr.TextFilter(name))))
func FilterBy[S, V any](source FilterSource[S, V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionFilterBy, source: source}
}

// Searchable marks the column searchable and infers scope and value from its row expression.
//
// Example: datatable.Column(w.Username, "users.fields.username", expr.AsSelected(name), datatable.Searchable(name))
func Searchable[S, V any](_ expr.RowValue[S, V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionSearchable}
}

// ExportAs binds a cell formatter, inferring scope and value from a selected expression.
// The expression supplies types only; exports format the DTO field value.
//
// Example: datatable.Column(w.Username, "users.fields.username", expr.AsSelected(name), datatable.ExportAs(expr.AsSelected(name), datatable.ScalarCell(text)))
func ExportAs[S, V any](_ expr.Selected[S, V], cell Cell[V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionExportAs, cell: cell}
}

// ColumnRegistration erases V only after ownership-safe declaration. Columns
// from a different source scope or response row cannot be registered together.
//
// Example: datatable.Text(w.Email, "members.email", Members.Email)
type ColumnRegistration[S, R any] struct{ declaration columnDeclaration[S, R] }

// FilterRegistration retains a named filter's typed source until table registration.
//
// Example: datatable.DefineFilter("email", "members.email", source)
type FilterRegistration[S any] struct{ declaration filterDeclaration[S] }

// DefineFilter declares an extra server filter (for example a related model's
// name) independently of displayed DTO columns. It shares the strict allowlist.
//
// Example: datatable.DefineFilter(name, label, source)
func DefineFilter[S, V any](name string, label i18n.MessageKey, source FilterSource[S, V]) FilterRegistration[S] {
	return FilterRegistration[S]{declaration: filterDeclaration[S]{name: name, label: label, info: source.info, build: source.enabled(), err: source.Validate()}}
}
