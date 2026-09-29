package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
type Cell[V any] struct {
	scalar   foundryhttp.URLScalarInfo
	nullable bool
	kind     cellKind
	format   func(context.Context, V, Presentation) (string, error)
	err      error
}

func ScalarCell[V any](codec foundryhttp.QueryCodec[V]) Cell[V] {
	info, err := foundryhttp.DescribePathCodec(codec)
	return Cell[V]{scalar: info, kind: scalarCellKind(info.Value), err: err, format: func(_ context.Context, v V, _ Presentation) (string, error) { return codec.Format(v) }}
}
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

type ColumnInfo struct {
	Name           string                  `json:"name"`
	Label          i18n.MessageKey         `json:"label"`
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
	order      func(Direction) query.ProjectionOrder[S]
	filter     *filterDeclaration[S]
	searchable bool
	cellInfo   *FilterInfo
	cell       func(context.Context, R, Presentation) (exportCell, error)
	err        error
}

// Column retains SQL scope S, response row R and exact field value V. Names and
// getters come from generated validation fields, not a duplicate string map.
type Column[S, R, V any] struct {
	declaration columnDeclaration[S, R]
	field       validation.Field[R, V]
}

func DefineColumn[S, R, V any](field validation.Field[R, V], label i18n.MessageKey) Column[S, R, V] {
	c := Column[S, R, V]{field: field, declaration: columnDeclaration[S, R]{name: field.Name(), label: label}}
	c.declaration.err = field.Validate()
	if err := label.Validate(); err != nil {
		c.declaration.err = err
	}
	return c
}
func (c Column[S, R, V]) SortBy(expression query.Expression[S, V]) Column[S, R, V] {
	c.declaration.order = func(direction Direction) query.ProjectionOrder[S] {
		if direction == Descending {
			return expression.Desc()
		}
		return expression.Asc()
	}
	return c
}
func (c Column[S, R, V]) FilterBy(source FilterSource[S, V]) Column[S, R, V] {
	c.declaration.filter = &filterDeclaration[S]{name: c.declaration.name, label: c.declaration.label, info: source.info, build: source.enabled(), err: source.Validate()}
	return c
}
func (c Column[S, R, V]) Searchable() Column[S, R, V] { c.declaration.searchable = true; return c }
func (c Column[S, R, V]) ExportAs(cell Cell[V]) Column[S, R, V] {
	if cell.err != nil {
		c.declaration.err = cell.err
	}
	if cell.format == nil {
		c.declaration.err = invalid("cell formatter is not defined")
	}
	c.declaration.cellInfo = &FilterInfo{Scalar: cell.scalar, Nullable: cell.nullable}
	field, kind := c.field, cell.kind
	c.declaration.cell = func(ctx context.Context, row R, p Presentation) (exportCell, error) {
		v, err := field.Select(row)
		if err != nil {
			return exportCell{}, err
		}
		text, err := cell.format(ctx, v, p)
		return exportCell{text: text, kind: kind}, err
	}
	return c
}

// ColumnRegistration erases V only after ownership-safe declaration. Columns
// from a different source scope or response row cannot be registered together.
type ColumnRegistration[S, R any] struct{ declaration columnDeclaration[S, R] }

func (c Column[S, R, V]) Registration() ColumnRegistration[S, R] {
	return ColumnRegistration[S, R]{declaration: c.declaration}
}

type FilterRegistration[S any] struct{ declaration filterDeclaration[S] }

// DefineFilter declares an extra server filter (for example a related model's
// name) independently of displayed DTO columns. It shares the strict allowlist.
func DefineFilter[S, V any](name string, label i18n.MessageKey, source FilterSource[S, V]) FilterRegistration[S] {
	return FilterRegistration[S]{declaration: filterDeclaration[S]{name: name, label: label, info: source.info, build: source.enabled(), err: source.Validate()}}
}
