package rows

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ValueSource retains exact scalar types across set inputs. Example: rows.UnionValues(a,b).
type ValueSource[V any] interface {
	valueData(*V) (ormcore.RecordSource, ormcore.Column[V])
}

// ValueRecord adapts one selected scalar without widening its type or granting record access.
// Example: rows.Values(rows.SelectScalar(source,expr.Row(id).Value())).
type ValueRecord[V any] struct {
	_           [0]*V
	scalarInput [0]struct{}
	core        ormcore.RecordSource
	column      ormcore.Column[V]
}

func (q ValueRecord[V]) valueData(*V) (ormcore.RecordSource, ormcore.Column[V]) {
	return q.core, q.column
}

type valueRecordDefinition struct{ record ormcore.Record }

// Values adapts a typed scalar selection for value set operations.
// Example: rows.Values(rows.SelectScalar(rows.From(Users.Query()),expr.Row(expr.Value(Users.ID)).Value())).
func Values[S, V any](q Scalar[S, V]) ValueRecord[V] {
	d := scalarRecord(q.column)
	r := ormcore.RecordSource{Source: q.source, Record: d.record}
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[V](), false)
	return ValueRecord[V]{core: r, column: q.column}
}

func scalarRecord[V any](column ormcore.Column[V]) *valueRecordDefinition {
	d := &valueRecordDefinition{}
	d.record = ormcore.Record{Fields: func() []ormcore.RecordField {
		return []ormcore.RecordField{{Name: "value", Sensitive: column.Sensitive, Get: func(p unsafe.Pointer) (driver.Value, error) { return column.Bind(*(*V)(p)) }, Decode: func(raw any) (driver.Value, error) {
			var v V
			scanner, ok := column.Scan(&v).(interface{ Scan(any) error })
			if !ok {
				return nil, ormcore.Invalid("cursor value requires a scanner")
			}
			if err := scanner.Scan(raw); err != nil {
				return nil, err
			}
			return column.Bind(v)
		}}}
	}, Type: reflect.TypeFor[V](), Owner: d, Columns: []querycore.Column{{Name: "value", Nullable: value.IsNullableType[V]()}}, Err: column.Err, Scan: func(row database.Row, p unsafe.Pointer) error {
		if column.Scan == nil {
			return ormcore.Invalid("value source requires a decoder")
		}
		return row.Scan(column.Scan((*V)(p)))
	}}
	if column.Scan == nil || column.Bind == nil || column.Clone == nil {
		d.record.Err = safeerror.Join("operation failed", d.record.Err, ormcore.Invalid("value source requires its codec"))
	}
	return d
}

func valueSource[V any](q ValueSource[V]) (ormcore.RecordSource, ormcore.Column[V]) {
	if ormcore.NilDescriptor(q) {
		return ormcore.RecordSource{Source: ormcore.Source{Err: ormcore.Invalid("value set requires two scalar sources")}}, ormcore.Column[V]{}
	}
	r, c := q.valueData(nil)
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[V](), false)
	if c.Scan == nil || c.Bind == nil || c.Clone == nil {
		r.Source.Err = safeerror.Join("operation failed", r.Source.Err, ormcore.Invalid("value set requires its codec"))
	}
	return r, c
}

// ValueSetQuery retains one exact scalar type in the combined output scope.
// Example: rows.ReadValues(values.Where(values.Value().Ne(id))).All(ctx,db).
type ValueSetQuery[V any] struct {
	_         [0]*V
	scalarSet [0]struct{}
	core      ormcore.RecordSource
	column    ormcore.Column[V]
}

func (q ValueSetQuery[V]) valueData(*V) (ormcore.RecordSource, ormcore.Column[V]) {
	return ormcore.SetRecord(q.core), q.column
}
func (q ValueSetQuery[V]) recordData(*V) ormcore.RecordSource { return ormcore.SetRecord(q.core) }
func (q ValueSetQuery[V]) rowSource(*Set[V]) ormcore.Source   { return ormcore.SetRecord(q.core).Source }
func combineValues[V any](a, b ValueSource[V], op querycore.SetOperator) ValueSetQuery[V] {
	left, c := valueSource(a)
	right, _ := valueSource(b)
	return ValueSetQuery[V]{core: ormcore.CombineRecords(left, right, op, reflect.TypeFor[Set[V]]()), column: c}
}

// Value exposes the combined scalar with its original codec and nullability.
// Example: expr.Row(values.Value()).Value().
func (q ValueSetQuery[V]) Value() expr.RowExpression[Set[V], V] {
	r := ormcore.SetRecord(q.core)
	e := ormcore.Expression{Node: querycore.FieldRef{Table: querycore.SetAlias, Column: "value"}, Refs: []ormcore.Reference{{Table: querycore.SetAlias, Owner: r.Record.Owner}}, Err: r.Source.Err}
	return expr.FoundryRowExpression[Set[V]](e, q.column, sqlowner.Expressions())
}

// Where filters combined values. Example: combined.Where(combined.Value().Eq(v)).
func (q ValueSetQuery[V]) Where(ps ...expr.Predicate[Set[V]]) ValueSetQuery[V] {
	q.core.Source = whereSource(q.core.Source, ps)
	return q
}

// OrderBy orders combined values. Example: combined.OrderBy(combined.Value().Asc()).
func (q ValueSetQuery[V]) OrderBy(os ...expr.ProjectionOrder[Set[V]]) ValueSetQuery[V] {
	q.core.Source = recordOrders(q.core.Source, os)
	return q
}

// Limit bounds combined values. Example: combined.Limit(10).
func (q ValueSetQuery[V]) Limit(n int) ValueSetQuery[V] {
	q.core.Source.Node.Limit = value.Set(n)
	return q
}

// Offset skips combined values. Example: combined.Offset(10).
func (q ValueSetQuery[V]) Offset(n int) ValueSetQuery[V] { q.core.Source.Node.Offset = n; return q }
