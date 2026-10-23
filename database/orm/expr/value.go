package expr

import (
	"database/sql/driver"
	"errors"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RowValue is a sealed value evaluated before grouping. Example: expr.Add(expr.Value(Users.Age),expr.Value(Users.Age)).
type RowValue[S, V any] interface{ rowValue(*S) rowData[V] }

type rowData[V any] struct {
	core   ormcore.Expression
	column ormcore.Column[V]
}

// RowExpression is a computed row value with exact owner and result types. Example: expr.Param(Users.Age,2).
type RowExpression[S, V any] struct {
	rowPhase [0]struct{}
	_        [0]*S
	data     rowData[V]
}

// Selected is a selected-phase value; it cannot become a row predicate. Example: expr.Count[User]().Value().
type Selected[S, V any] struct {
	selectedPhase [0]struct{}
	_             [0]*S
	data          rowData[V]
}

func (e Expression[S, V, P]) rowValue(*S) rowData[P] {
	if e.projection == nil {
		return rowData[P]{core: ormcore.Expression{Err: ormcore.Invalid("uninitialized expression")}}
	}
	return rowData[P]{core: e.core, column: e.projection()}
}
func (e RowExpression[S, V]) rowValue(*S) rowData[V] { return e.data }

func rowInput[S, V any](v RowValue[S, V]) RowExpression[S, V] {
	if ormcore.NilDescriptor(v) {
		return RowExpression[S, V]{data: rowData[V]{core: ormcore.Expression{Err: ormcore.Invalid("missing expression")}}}
	}
	return RowExpression[S, V]{data: v.rowValue(nil)}
}

// Select promotes a row value to the selected phase. Example: expr.Select(expr.Value(Users.Age)).
func Select[S, V any](v RowValue[S, V]) Selected[S, V] { return rowInput(v).Value() }

// Value promotes a computed row value to the selected phase. Example: expr.Add(a,b).Value().
func (e RowExpression[S, V]) Value() Selected[S, V] { return Selected[S, V]{data: e.data} }

// Param snapshots a bound non-null value using a column's comparison codec. Example: expr.Param(Users.Nickname,"Ada").
func Param[S, V, P any](column orm.ExpressionColumn[S, V, P], v V) RowExpression[S, V] {
	c, _ := orm.FoundryColumn(column, sqlowner.Metadata())
	return parameter[S](rowData[V]{column: c, core: ormcore.Expression{Refs: []ormcore.Reference{c.Reference}, Err: c.Err}}, v)
}

// Param captures this row expression's exact value type. Example: expr.Add(a,b).Param(2).
func (e RowExpression[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](e.data, v)
}

// Param captures this selected value's type without evaluating it. Example: expr.Count[User]().Value().Param(0).
func (e Selected[S, V]) Param(v V) RowExpression[S, V] {
	return parameter[S](e.data, v)
}

func parameter[S, V any](exemplar rowData[V], v V) RowExpression[S, V] {
	c := exemplar.column
	err := errors.Join(exemplar.core.Err, c.Err)
	var node querycore.ParameterNode
	if c.Bind == nil || c.Clone == nil {
		err = errors.Join(err, ormcore.Invalid("uninitialized parameter codec"))
	} else if err == nil {
		node.Value, node.Err = c.Bind(c.Clone(v))
	}
	node.Kind = c.Kind
	refs, referenceErr := ormcore.MergeReferences(exemplar.core.Refs, []ormcore.Reference{{Scope: reflect.TypeFor[S]()}})
	node.Err = errors.Join(err, node.Err, referenceErr)
	return RowExpression[S, V]{data: rowData[V]{core: ormcore.Expression{Node: node, Refs: refs, ReferenceFree: true, Err: node.Err}, column: c}}
}

func columnCodec[V any](c codec.Codec[V]) ormcore.Column[V] {
	return ormcore.Column[V]{Kind: c.ParameterType(), Nullable: value.IsNullableType[V](), Bind: c.Bind, Clone: c.Clone, Scan: func(v *V) any { return c.Scan(v) }, Err: c.Validate()}
}

// NullableRow explicitly widens a row value by one nullable layer. Example: expr.NullableRow(expr.Value(Users.Age)).
func NullableRow[S, V any](v RowValue[S, V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{data: nullableData(rowInput(v).data)}
}

// Nullable widens a selected value by one nullable layer without changing its phase.
// Example: expr.Nullable(expr.Count[User]().Value()).
func Nullable[S, V any](e Selected[S, V]) Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: nullableData(e.data)}
}

func nullableData[V any](e rowData[V]) rowData[value.Nullable[V]] {
	c := e.column
	out := ormcore.Column[value.Nullable[V]]{Kind: c.Kind, Nullable: true, Err: c.Err}
	out.Bind = func(v value.Nullable[V]) (driver.Value, error) {
		x, ok := v.Get()
		if !ok {
			return nil, nil
		}
		if c.Bind == nil {
			return nil, ormcore.Invalid("uninitialized nullable codec")
		}
		return c.Bind(x)
	}
	out.Clone = func(v value.Nullable[V]) value.Nullable[V] {
		x, ok := v.Get()
		if !ok {
			return value.Null[V]()
		}
		if c.Clone == nil {
			return v
		}
		return value.Of(c.Clone(x))
	}
	if value.IsNullableType[V]() {
		e.core.Err = errors.Join(e.core.Err, ormcore.Invalid("value is already nullable"))
		out.Err = e.core.Err
	}
	return rowData[value.Nullable[V]]{core: e.core, column: out}
}

// NullFor captures SQL NULL with an exemplar's type. Example: expr.NullFor(expr.Value(Users.Age)).
func NullFor[S, V any](v RowValue[S, V]) RowExpression[S, value.Nullable[V]] {
	return NullableRow(v).Param(value.Null[V]())
}

// FoundrySelected exposes sealed selected metadata to rows. Example: FoundrySelected(e,sqlowner.Expressions()).
func FoundrySelected[S, V any](e Selected[S, V], seal sqlowner.ExpressionSeal) ormcore.Expression {
	if !seal.Valid() {
		return ormcore.Expression{Err: ormcore.Invalid("invalid SQL owner capability")}
	}

	return e.data.core
}
