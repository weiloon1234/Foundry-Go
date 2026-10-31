package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Cell preserves the exact DTO field type through export formatting. Ordinary
// scalar cells reuse transport codecs; nullable cells render SQL NULL as empty.
// The table's JSON graph remains authoritative for public field metadata.
// XLSX writes scalar integer, float, decimal, boolean, date and date-time
// cells as typed spreadsheet values when the codec text is exactly
// representable; FormatWith output is always text.
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

// FormatWith explicitly customizes human presentation without replacing the
// field's JSON or filter contract. It must be concurrency-safe and bounded.
// Custom presentation is exported as text in every format.
func (c Cell[V]) FormatWith(format func(context.Context, V, Presentation) (string, error)) Cell[V] {
	if format == nil {
		c.err = invalid("cell formatter is missing")
	} else {
		c.format = format
		c.kind = textCell
	}
	return c
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
	order      func(Direction) expr.ProjectionOrder[S]
	filter     *filterDeclaration[S]
	searchable bool
	cellInfo   *FilterInfo
	cell       func(context.Context, R, Presentation) (exportCell, error)
	err        error
}

type columnExecution[S, R any] struct {
	order  func(Direction) expr.ProjectionOrder[S]
	filter func(Operator, []string) (condition[S], error)
	cell   func(context.Context, R, Presentation) (exportCell, error)
}

// ColumnOption preserves the source scope and DTO field type until registration.
// Construct options with SortBy, FilterBy, Searchable and ExportAs.
//
// Example: datatable.SortBy(expr.AsSelected(name))
type ColumnOption[S, V any] struct {
	kind   optionKind
	order  func(Direction) expr.ProjectionOrder[S]
	source FilterSource[S, V]
	cell   Cell[V]
}

type optionKind uint8

const (
	optionSortBy optionKind = iota + 1
	optionFilterBy
	optionSearchable
	optionExportAs
)

// SortBy binds a selected value of the same scope and DTO field type.
//
// Example: datatable.DefineColumn[UserScope](w.Username, key, datatable.SortBy(expr.AsSelected(name)))
func SortBy[S, V any](expression expr.Selected[S, V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionSortBy, order: func(direction Direction) expr.ProjectionOrder[S] {
		if direction == Descending {
			return expr.DescValue(expression)
		}
		return expr.AscValue(expression)
	}}
}

// FilterBy binds a filter source for the column.
//
// Example: datatable.DefineColumn[UserScope](w.Username, key, datatable.FilterBy(datatable.Where(Text, expr.TextFilter(name))))
func FilterBy[S, V any](source FilterSource[S, V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionFilterBy, source: source}
}

// Searchable marks the column searchable and infers scope and value from its row expression.
//
// Example: datatable.DefineColumn[UserScope](w.Username, key, datatable.Searchable(name))
func Searchable[S, V any](_ expr.RowValue[S, V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionSearchable}
}

// ExportAs binds a cell formatter, inferring scope and value from a selected expression.
// The expression supplies types only; exports format the DTO field value.
//
// Example: datatable.DefineColumn[UserScope](w.Username, key, datatable.ExportAs(expr.AsSelected(name), datatable.ScalarCell(Text)))
func ExportAs[S, V any](_ expr.Selected[S, V], cell Cell[V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionExportAs, cell: cell}
}

// DefineColumn binds an exact DTO field to typed source options and snapshots its declaration.
//
// Example: datatable.DefineColumn[User](field, label, options...)
func DefineColumn[S, R, V any](field validation.Field[R, V], label i18n.MessageKey, options ...ColumnOption[S, V]) ColumnRegistration[S, R] {
	c := columnDeclaration[S, R]{name: validation.FieldName(field), label: label}
	c.err = validation.ValidateField(field)
	if err := label.Validate(); err != nil && c.err == nil {
		c.err = err
	}
	for _, opt := range options {
		switch opt.kind {
		case optionSearchable:
			c.searchable = true
		case optionSortBy:
			c.order = opt.order
		case optionFilterBy:
			source := opt.source
			err := source.Validate()
			if err != nil && c.err == nil {
				c.err = err
			}
			c.filter = &filterDeclaration[S]{name: c.name, label: c.label, info: source.info, build: source.enabled(), err: err}
		case optionExportAs:
			cell := opt.cell
			err := cell.err
			if cell.format == nil && err == nil {
				err = invalid("cell formatter is not defined")
			}
			if err != nil && c.err == nil {
				c.err = err
			}
			c.cellInfo = &FilterInfo{Scalar: cell.scalar, Nullable: cell.nullable}
			c.cell = func(ctx context.Context, row R, p Presentation) (exportCell, error) {
				v, err := validation.Select(field, row)
				if err != nil {
					return exportCell{}, err
				}
				text, err := cell.format(ctx, v, p)
				return exportCell{text: text, kind: cell.kind}, err
			}
		default:
			if c.err == nil {
				c.err = invalid("column option is missing")
			}
		}
	}
	return ColumnRegistration[S, R]{declaration: c}
}

// ColumnRegistration erases V only after ownership-safe declaration. Columns
// from a different source scope or response row cannot be registered together.
//
// Example: datatable.DefineColumn[User](field, "members.email", datatable.SortBy(expr.AsSelected(name)))
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
