package datatable

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ColumnFeature removes a default capability from a direct column constructor.
// Zero or unknown values fail table declaration validation; repeated valid flags
// are harmless. For joined, computed, or custom-codec columns, use DefineColumn.
// Example: datatable.Text(w.Name, "users.fields.name", Users.Name, datatable.NoExport).
type ColumnFeature uint8

const (
	// NoSort disables sorting on a direct column constructor.
	NoSort ColumnFeature = iota + 1
	// NoFilter disables filtering (and search) on a direct column constructor.
	NoFilter
	// NoSearch disables text search while keeping the pattern filter on Text and NullableText; passing NoSearch to any other constructor fails declaration validation.
	NoSearch
	// NoExport excludes a direct column constructor from CSV/XLSX export.
	NoExport
)

func featureFlags(defaultSearch bool, features []ColumnFeature) (sort, where, search, export bool, err error) {
	sort, where, search, export = true, true, defaultSearch, true
	for _, f := range features {
		switch f {
		case NoSort:
			sort = false
		case NoFilter:
			where = false
			search = false
		case NoSearch:
			if !defaultSearch {
				return false, false, false, false, invalid("column feature NoSearch applies only to text columns")
			}
			search = false
		case NoExport:
			export = false
		default:
			if f == 0 {
				return false, false, false, false, invalid("column feature is missing")
			}
			return false, false, false, false, invalid("column feature is unknown")
		}
	}
	return sort, where, search, export, nil
}

var (
	defaultStringQuery = http.StringQuery[string]()
	defaultBoolQuery   = http.BoolQuery[bool]()
)

func stringQueryCodec[V ~string]() http.QueryCodec[V] {
	if c, ok := any(defaultStringQuery).(http.QueryCodec[V]); ok {
		return c
	}
	return http.StringQuery[V]()
}

func boolQueryCodec[V ~bool]() http.QueryCodec[V] {
	if c, ok := any(defaultBoolQuery).(http.QueryCodec[V]); ok {
		return c
	}
	return http.BoolQuery[V]()
}

// Text registers a non-null text column on Spec.Columns with sort, filter, search, and export enabled by default.
// Example: datatable.Text(w.Name, "users.fields.name", Users.Name).
func Text[S, R, I, P any, V ~string, K orm.RequiredTextColumnKind, Role orm.ColumnRole](field validation.Field[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.TextRow(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, search, export, err := featureFlags(true, features)
	var opts [4]ColumnOption[S, V]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	var codec http.QueryCodec[V]
	if where || export {
		codec = stringQueryCodec[V]()
	}
	if where {
		opts[n] = FilterBy(Where(codec, expr.TextFilter(source)))
		n++
	}
	if search {
		opts[n] = ColumnOption[S, V]{kind: optionSearchable}
		n++
	}
	if export {
		opts[n] = ExportAs(selected, ScalarCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// Number registers a non-null numeric column on Spec.Columns with sort, ordered filter, and export enabled by default.
// Example: datatable.Number(w.TotalCents, "orders.fields.total", Orders.TotalCents, intCodec).
func Number[S, R, I, P any, V orm.NumberValue, K orm.RequiredNumericColumnKind, Role orm.ColumnRole](field validation.Field[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec http.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.OrderedColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, V]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	if where {
		opts[n] = FilterBy(Where(codec, expr.OrderFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, ScalarCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// Time registers a non-null temporal column on Spec.Columns with sort, ordered filter, and export enabled by default.
// Example: datatable.Time(w.CreatedAt, "orders.fields.created_at", Orders.CreatedAt, timeCodec).
func Time[S, R, I, P any, V orm.TimeValue, K orm.RequiredTimeColumnKind, Role orm.ColumnRole](field validation.Field[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec http.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.OrderedColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, V]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	if where {
		opts[n] = FilterBy(Where(codec, expr.OrderFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, ScalarCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// Enum registers a non-null enum column on Spec.Columns with sort, equality filter, and export enabled by default.
// Example: datatable.Enum(w.Status, "orders.fields.status", Orders.Status, statusCodec).
func Enum[S, R, I, P any, K orm.RequiredEnumColumnKind, Role orm.ColumnRole, V interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[V]
}](field validation.Field[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec http.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.CompareColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, V]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	if where {
		opts[n] = FilterBy(Where(codec, expr.CompareFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, ScalarCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// Bool registers a non-null boolean column on Spec.Columns with sort, equality filter, and export enabled by default.
// Example: datatable.Bool(w.Active, "users.fields.active", Users.Active).
func Bool[S, R, I, P any, V ~bool, K orm.RequiredScalarColumnKind, Role orm.ColumnRole](field validation.Field[R, V], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.CompareColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, V]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	var codec http.QueryCodec[V]
	if where || export {
		codec = boolQueryCodec[V]()
	}
	if where {
		opts[n] = FilterBy(Where(codec, expr.CompareFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, ScalarCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// NullableText registers a nullable text column on Spec.Columns with sort, nullable filter, search, and export enabled by default.
// Example: datatable.NullableText(w.Nickname, "users.fields.nickname", Users.Nickname).
func NullableText[S, R, I, P any, V ~string, K orm.NullableTextColumnKind, Role orm.ColumnRole](field validation.Field[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.TextNullableRow(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, search, export, err := featureFlags(true, features)
	var opts [4]ColumnOption[S, value.Nullable[V]]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	var codec http.QueryCodec[V]
	if where || export {
		codec = stringQueryCodec[V]()
	}
	if where {
		opts[n] = FilterBy(NullableWhere(codec, expr.NullableTextFilter(source)))
		n++
	}
	if search {
		opts[n] = ColumnOption[S, value.Nullable[V]]{kind: optionSearchable}
		n++
	}
	if export {
		opts[n] = ExportAs(selected, NullableCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// NullableNumber registers a nullable numeric column on Spec.Columns with sort, nullable ordered filter, and export enabled by default.
// Example: datatable.NullableNumber(w.Discount, "orders.fields.discount", Orders.Discount, intCodec).
func NullableNumber[S, R, I, P any, V orm.NumberValue, K orm.NullableNumericColumnKind, Role orm.ColumnRole](field validation.Field[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec http.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.NullableOrderedColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, value.Nullable[V]]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	if where {
		opts[n] = FilterBy(NullableWhere(codec, expr.NullableOrderFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, NullableCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// NullableTime registers a nullable temporal column on Spec.Columns with sort, nullable ordered filter, and export enabled by default.
// Example: datatable.NullableTime(w.PaidAt, "orders.fields.paid_at", Orders.PaidAt, timeCodec).
func NullableTime[S, R, I, P any, V orm.TimeValue, K orm.NullableTimeColumnKind, Role orm.ColumnRole](field validation.Field[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec http.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.NullableOrderedColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, value.Nullable[V]]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	if where {
		opts[n] = FilterBy(NullableWhere(codec, expr.NullableOrderFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, NullableCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}

// NullableEnum registers a nullable enum column on Spec.Columns with sort, nullable equality filter, and export enabled by default.
// Example: datatable.NullableEnum(w.Tier, "users.fields.tier", Users.Tier, tierCodec).
func NullableEnum[S, R, I, P any, K orm.NullableEnumColumnKind, Role orm.ColumnRole, V interface {
	enum.Scalar
	EnumDescriptor() enum.Descriptor[V]
}](field validation.Field[R, value.Nullable[V]], label i18n.MessageKey, column orm.ColumnHandle[S, V, I, P, K, Role], codec http.QueryCodec[V], features ...ColumnFeature) ColumnRegistration[S, R] {
	source := expr.NullableCompareColumn(column)
	selected := expr.AsSelected(expr.Row(source))
	sort, where, _, export, err := featureFlags(false, features)
	var opts [3]ColumnOption[S, value.Nullable[V]]
	n := 0
	if sort {
		opts[n] = SortBy(selected)
		n++
	}
	if where {
		opts[n] = FilterBy(NullableWhere(codec, expr.NullableCompareFilter(source)))
		n++
	}
	if export {
		opts[n] = ExportAs(selected, NullableCell(codec))
		n++
	}
	reg := DefineColumn[S](field, label, opts[:n]...)
	if err != nil && reg.declaration.err == nil {
		reg.declaration.err = err
	}
	return reg
}
