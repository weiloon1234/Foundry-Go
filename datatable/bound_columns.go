package datatable

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DynamicSlot is a typed token pairing one request-dependent column's scope S and
// value type V with its per-request expression builder in Spec.Dynamic. It is the
// plan's own slot, so one identity serves the table and its query plan.
//
// Example: slot := datatable.NewDynamicSlot[EntityListScope, string]()
type DynamicSlot[S, V any] struct {
	slot rows.FoundryDynamicSlot[S, V]
}

// NewDynamicSlot creates a distinct typed slot for a request-dependent column selection.
//
// Example: slot := datatable.NewDynamicSlot[EntityListScope, string]()
func NewDynamicSlot[S, V any]() DynamicSlot[S, V] {
	return DynamicSlot[S, V]{slot: rows.FoundryNewDynamicSlot[S, V](sqlowner.Queries())}
}

// identity returns the slot as a comparable value shared by every value type V,
// or nil for a slot not issued by NewDynamicSlot.
func (s DynamicSlot[S, V]) identity() any {
	if s.slot == (rows.FoundryDynamicSlot[S, V]{}) {
		return nil
	}
	return s.slot
}

// DynamicBinding binds one DynamicSlot to its authority-typed per-request SQL expression.
//
// Example: var binding datatable.DynamicBinding[EntityListScope, AdminAuthority]
type DynamicBinding[S, A any] struct {
	slot  any
	build func(context.Context, A) rows.FoundryDynamicExpression[S]
	err   error
}

// BindDynamic pairs a DynamicSlot with its per-request expression builder.
// S, A, and V are inferred from slot and build. The table calls build once per
// query, count or export request, after authorization, and never at declaration.
//
// Example: datatable.BindDynamic(slot, func(ctx context.Context, a AdminAuthority) expr.Selected[EntityListScope, string] { return localizedName(a.Locale) })
func BindDynamic[S, A, V any](slot DynamicSlot[S, V], build func(context.Context, A) expr.Selected[S, V]) DynamicBinding[S, A] {
	identity := slot.identity()
	if identity == nil || build == nil {
		return DynamicBinding[S, A]{slot: identity, err: invalid("dynamic column binding requires a declared slot and builder")}
	}
	planSlot := slot.slot
	return DynamicBinding[S, A]{
		slot: identity,
		build: func(ctx context.Context, authority A) rows.FoundryDynamicExpression[S] {
			return rows.FoundryDynamicValue(planSlot, build(ctx, authority), sqlowner.Queries())
		},
	}
}

// ScanWith overrides the SQL scan codec on Column, DynamicColumn, or HiddenSelected
// when an expression needs a custom database codec. The selected expression keeps
// its own declaration error and enum check.
//
// Example: datatable.ScanWith[OrderScope](orm.Codec(codec.Signed[int64]()))
func ScanWith[S, V any](option orm.ColumnOption[V]) ColumnOption[S, V] {
	return ColumnOption[S, V]{kind: optionScanWith, scan: rows.FoundryScanWith(option, sqlowner.Queries())}
}

func makeOrderFunc[S, V any](expression expr.Selected[S, V]) func(Direction, expr.Collation) expr.ProjectionOrder[S] {
	return func(direction Direction, collation expr.Collation) expr.ProjectionOrder[S] {
		selected := expr.FoundryDefaultCollation(expression, collation, sqlowner.Expressions())
		if direction == Descending {
			return expr.DescValue(selected)
		}
		return expr.AscValue(selected)
	}
}

func makeBindingCell[R, V any](field validation.Binding[R, V], cell Cell[V]) func(context.Context, *R, Presentation) (exportCell, error) {
	return func(ctx context.Context, row *R, p Presentation) (exportCell, error) {
		ptr := validation.BoundPointer(field, row)
		if ptr == nil {
			return exportCell{}, invalid("row field binding returned a nil pointer")
		}
		text, err := cell.format(ctx, *ptr, p)
		return exportCell{text: text, kind: cell.kind}, err
	}
}

func buildColumnOptions[S, R, V any](field validation.Binding[R, V], label i18n.MessageKey, options []ColumnOption[S, V]) (columnDeclaration[S, R], []rows.FoundryScan[V]) {
	c := columnDeclaration[S, R]{name: validation.BindingName(field), label: label}
	c.err = validation.ValidateBinding(field)
	if err := label.Validate(); err != nil && c.err == nil {
		c.err = err
	}
	var scan []rows.FoundryScan[V]
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
			c.cell = makeBindingCell(field, cell)
		case optionScanWith:
			if scan != nil && c.err == nil {
				c.err = invalid("column scan codec option is repeated")
			}
			scan = []rows.FoundryScan[V]{opt.scan}
		default:
			if c.err == nil {
				c.err = invalid("column option is missing")
			}
		}
	}
	return c, scan
}

// Column declares one displayed column backed by an explicit SQL selection (joined,
// computed, grouped aggregate, custom filter, or custom scan codec). It is the
// general form of the direct constructors such as Text and Number: sorting,
// filtering, search and export are enabled only by the options passed.
// S, R, and V are inferred from field and selected; no explicit type arguments are needed.
//
// Example: datatable.Column(w.Total, "orders.fields.total", expr.AsSelected(total), datatable.SortBy(expr.AsSelected(total)))
func Column[S, R, V any](field validation.Binding[R, V], label i18n.MessageKey, selected expr.Selected[S, V], options ...ColumnOption[S, V]) ColumnRegistration[S, R] {
	c, scan := buildColumnOptions(field, label, options)
	c.bind = func(pb rows.FoundryPlanBuilder[S, R], row *R) int {
		return rows.FoundryBindPlanField(pb, validation.BoundPointer(field, row), selected, scan...)
	}
	return ColumnRegistration[S, R]{declaration: c}
}

// DynamicColumn declares one displayed column whose SQL selection is supplied per
// request through Spec.Dynamic using the same DynamicSlot. Its SortBy stays an
// explicit static expression, so a table can show a request-localized label and
// sort by a stable code.
//
// Example: datatable.DynamicColumn(w.Category, "capture.fields.category", categorySlot, datatable.SortBy(expr.AsSelected(categoryCode)))
func DynamicColumn[S, R, V any](field validation.Binding[R, V], label i18n.MessageKey, slot DynamicSlot[S, V], options ...ColumnOption[S, V]) ColumnRegistration[S, R] {
	c, scan := buildColumnOptions(field, label, options)
	c.dynamicSlot = slot.identity()
	if c.dynamicSlot == nil && c.err == nil {
		c.err = invalid("dynamic column requires a declared slot")
	}
	planSlot := slot.slot
	c.bind = func(pb rows.FoundryPlanBuilder[S, R], row *R) int {
		return rows.FoundryBindPlanDynamic(pb, validation.BoundPointer(field, row), planSlot, scan...)
	}
	return ColumnRegistration[S, R]{declaration: c}
}

// Hidden selects a direct model column (such as a primary key, foreign key, or hidden
// scalar) into the row DTO without exposing it as a displayed table column.
//
// Example: datatable.Hidden(w.ID, a.Key())
func Hidden[S, R, V, I, P any, K orm.ColumnKind, Role orm.ColumnRole](field validation.Binding[R, P], column orm.ColumnHandle[S, V, I, P, K, Role]) ColumnRegistration[S, R] {
	return HiddenSelected(field, expr.AsSelected(expr.Row(expr.Value(column))))
}

// HiddenSelected selects a joined or computed expression into the row DTO without
// exposing it as a displayed table column. It accepts only the ScanWith option.
//
// Example: datatable.HiddenSelected(w.LotID, expr.AsSelected(expr.Row(expr.At(lot, Lots.Key()))))
func HiddenSelected[S, R, V any](field validation.Binding[R, V], selected expr.Selected[S, V], options ...ColumnOption[S, V]) ColumnRegistration[S, R] {
	c := columnDeclaration[S, R]{
		name:   validation.BindingName(field),
		hidden: true,
		err:    validation.ValidateBinding(field),
	}
	var scan []rows.FoundryScan[V]
	for _, opt := range options {
		switch opt.kind {
		case optionScanWith:
			if scan != nil && c.err == nil {
				c.err = invalid("column scan codec option is repeated")
			}
			scan = []rows.FoundryScan[V]{opt.scan}
		default:
			if c.err == nil {
				c.err = invalid("hidden columns only accept ScanWith options")
			}
		}
	}
	c.bind = func(pb rows.FoundryPlanBuilder[S, R], row *R) int {
		return rows.FoundryBindPlanField(pb, validation.BoundPointer(field, row), selected, scan...)
	}
	return ColumnRegistration[S, R]{declaration: c}
}

func directColumn[S, R, V any](
	field validation.Binding[R, V],
	label i18n.MessageKey,
	selected expr.Selected[S, V],
	sort, where, search, export bool,
	flagErr error,
	filter FilterSource[S, V],
	cell Cell[V],
) ColumnRegistration[S, R] {
	c := columnDeclaration[S, R]{
		name:  validation.BindingName(field),
		label: label,
		err:   validation.ValidateBinding(field),
	}
	if err := label.Validate(); err != nil && c.err == nil {
		c.err = err
	}
	if flagErr != nil && c.err == nil {
		c.err = flagErr
	}
	if sort {
		c.order = makeOrderFunc(selected)
	}
	if where {
		err := filter.Validate()
		if err != nil && c.err == nil {
			c.err = err
		}
		c.filter = &filterDeclaration[S]{name: c.name, label: c.label, info: filter.info, build: filter.enabled(), err: err}
	}
	if search {
		c.searchable = true
	}
	if export {
		err := cell.err
		if cell.format == nil && err == nil {
			err = invalid("cell formatter is not defined")
		}
		if err != nil && c.err == nil {
			c.err = err
		}
		c.cellInfo = &FilterInfo{Scalar: cell.scalar, Nullable: cell.nullable}
		c.cell = makeBindingCell(field, cell)
	}
	c.bind = func(pb rows.FoundryPlanBuilder[S, R], row *R) int {
		return rows.FoundryBindPlanField(pb, validation.BoundPointer(field, row), selected)
	}
	return ColumnRegistration[S, R]{declaration: c}
}

// Text registers a non-null text column with sort, filter, search, export, and SQL scan binding enabled by default.
// Example: datatable.Text(w.Name, "users.fields.name", Users.Name).
func Text[S, R, I, P any, V ~string, K orm.RequiredTextColumnKind, Role orm.ColumnRole](field validation.Binding[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.TextRow(column)
	return buildTextColumn(field, label, source, features)
}

func buildTextColumn[S, R any, V ~string](field validation.Binding[R, V], label i18n.MessageKey, source expr.TextRowExpression[S, V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, search, export, err := featureFlags(true, features)
	var codec foundryhttp.QueryCodec[V]
	if where || export {
		codec = stringQueryCodec[V]()
	}
	var filter FilterSource[S, V]
	if where {
		filter = Where(codec, expr.TextFilter(source))
	}
	var cell Cell[V]
	if export {
		cell = ScalarCell(codec)
	}
	return directColumn(field, label, selected, sort, where, search, export, err, filter, cell)
}

// Number registers a non-null numeric column with sort, ordered filter, export, and SQL scan binding enabled by default.
// Example: datatable.Number(w.TotalCents, "orders.fields.total", Orders.TotalCents, intCodec).
func Number[S, R, I, P any, V orm.NumberValue, K orm.RequiredNumericColumnKind, Role orm.ColumnRole](field validation.Binding[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec foundryhttp.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.OrderedColumn(column)
	return buildOrderedColumn(field, label, source, codec, features)
}

// Time registers a non-null temporal column with sort, ordered filter, export, and SQL scan binding enabled by default.
// Example: datatable.Time(w.CreatedAt, "orders.fields.created_at", Orders.CreatedAt, timeCodec).
func Time[S, R, I, P any, V orm.TimeValue, K orm.RequiredTimeColumnKind, Role orm.ColumnRole](field validation.Binding[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec foundryhttp.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.OrderedColumn(column)
	return buildOrderedColumn(field, label, source, codec, features)
}

func buildOrderedColumn[S, R any, V expr.OrderedScalar](field validation.Binding[R, V], label i18n.MessageKey, source expr.OrderedRowExpression[S, V], codec foundryhttp.QueryCodec[V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var filter FilterSource[S, V]
	if where {
		filter = Where(codec, expr.OrderFilter(source))
	}
	var cell Cell[V]
	if export {
		cell = ScalarCell(codec)
	}
	return directColumn(field, label, selected, sort, where, false, export, err, filter, cell)
}

// Enum registers a non-null enum column with sort, equality filter, export, and SQL scan binding enabled by default.
// Example: datatable.Enum(w.Status, "orders.fields.status", Orders.Status, statusCodec).
func Enum[S, R, I, P any, K orm.RequiredEnumColumnKind, Role orm.ColumnRole, V interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[V]
}](field validation.Binding[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec foundryhttp.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.CompareColumn(column)
	return buildCompareColumn(field, label, source, codec, features)
}

func buildCompareColumn[S, R, V any](field validation.Binding[R, V], label i18n.MessageKey, source expr.RowComparison[S, V], codec foundryhttp.QueryCodec[V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var filter FilterSource[S, V]
	if where {
		filter = Where(codec, expr.CompareFilter(source))
	}
	var cell Cell[V]
	if export {
		cell = ScalarCell(codec)
	}
	return directColumn(field, label, selected, sort, where, false, export, err, filter, cell)
}

// Bool registers a non-null boolean column with sort, equality filter, export, and SQL scan binding enabled by default.
// Example: datatable.Bool(w.Active, "users.fields.active", Users.Active).
func Bool[S, R, I, P any, V ~bool, K orm.RequiredScalarColumnKind, Role orm.ColumnRole](field validation.Binding[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.CompareColumn(column)
	return buildBoolColumn(field, label, source, features)
}

func buildBoolColumn[S, R any, V ~bool](field validation.Binding[R, V], label i18n.MessageKey, source expr.RowComparison[S, V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var codec foundryhttp.QueryCodec[V]
	if where || export {
		codec = boolQueryCodec[V]()
	}
	var filter FilterSource[S, V]
	if where {
		filter = Where(codec, expr.CompareFilter(source))
	}
	var cell Cell[V]
	if export {
		cell = ScalarCell(codec)
	}
	return directColumn(field, label, selected, sort, where, false, export, err, filter, cell)
}

// NullableText registers a nullable text column with sort, nullable filter, search, export, and SQL scan binding enabled by default.
// Example: datatable.NullableText(w.Nickname, "users.fields.nickname", Users.Nickname).
func NullableText[S, R, I, P any, V ~string, K orm.NullableTextColumnKind, Role orm.ColumnRole](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.TextNullableRow(column)
	return buildNullableTextColumn(field, label, source, features)
}

func buildNullableTextColumn[S, R any, V ~string](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, source expr.NullableTextRowExpression[S, V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, search, export, err := featureFlags(true, features)
	var codec foundryhttp.QueryCodec[V]
	if where || export {
		codec = stringQueryCodec[V]()
	}
	var filter FilterSource[S, value.Nullable[V]]
	if where {
		filter = NullableWhere(codec, expr.NullableTextFilter(source))
	}
	var cell Cell[value.Nullable[V]]
	if export {
		cell = NullableCell(codec)
	}
	return directColumn(field, label, selected, sort, where, search, export, err, filter, cell)
}

// NullableNumber registers a nullable numeric column with sort, nullable ordered filter, export, and SQL scan binding enabled by default.
// Example: datatable.NullableNumber(w.Discount, "orders.fields.discount", Orders.Discount, intCodec).
func NullableNumber[S, R, I, P any, V orm.NumberValue, K orm.NullableNumericColumnKind, Role orm.ColumnRole](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec foundryhttp.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.NullableOrderedColumn(column)
	return buildNullableOrderedColumn(field, label, source, codec, features)
}

// NullableTime registers a nullable temporal column with sort, nullable ordered filter, export, and SQL scan binding enabled by default.
// Example: datatable.NullableTime(w.PaidAt, "orders.fields.paid_at", Orders.PaidAt, timeCodec).
func NullableTime[S, R, I, P any, V orm.TimeValue, K orm.NullableTimeColumnKind, Role orm.ColumnRole](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec foundryhttp.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.NullableOrderedColumn(column)
	return buildNullableOrderedColumn(field, label, source, codec, features)
}

func buildNullableOrderedColumn[S, R any, V expr.OrderedScalar](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, source expr.NullableOrderedRowExpression[S, V], codec foundryhttp.QueryCodec[V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var filter FilterSource[S, value.Nullable[V]]
	if where {
		filter = NullableWhere(codec, expr.NullableOrderFilter(source))
	}
	var cell Cell[value.Nullable[V]]
	if export {
		cell = NullableCell(codec)
	}
	return directColumn(field, label, selected, sort, where, false, export, err, filter, cell)
}

// NullableEnum registers a nullable enum column with sort, nullable equality filter, export, and SQL scan binding enabled by default.
// Example: datatable.NullableEnum(w.Tier, "users.fields.tier", Users.Tier, tierCodec).
func NullableEnum[S, R, I, P any, K orm.NullableEnumColumnKind, Role orm.ColumnRole, V interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[V]
}](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec foundryhttp.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.NullableCompareColumn(column)
	return buildNullableCompareColumn(field, label, source, codec, features)
}

func buildNullableCompareColumn[S, R, V any](field validation.Binding[R, value.Nullable[V]], label i18n.MessageKey, source expr.RowComparison[S, value.Nullable[V]], codec foundryhttp.QueryCodec[V], features []ColumnFeature) ColumnRegistration[S, R] {
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var filter FilterSource[S, value.Nullable[V]]
	if where {
		filter = NullableWhere(codec, expr.NullableCompareFilter(source))
	}
	var cell Cell[value.Nullable[V]]
	if export {
		cell = NullableCell(codec)
	}
	return directColumn(field, label, selected, sort, where, false, export, err, filter, cell)
}
