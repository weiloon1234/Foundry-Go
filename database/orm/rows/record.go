package rows

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RecordSource carries a complete declared result, separately from its input scope.
// Example: rows.AsRecord[tag](rows.RecordOf(report),"report").
type RecordSource[R any] interface{ recordData(*R) ormcore.RecordSource }

// Record is a complete ordinary result adapter without execution or mutation methods.
// Example: rows.ModelRecord(Orders.Query()).
type Record[R any] struct {
	_              [0]*R
	ordinaryRecord [0]struct{}
	core           ormcore.RecordSource
}

func (r Record[R]) recordData(*R) ormcore.RecordSource { return r.core }

// RecordOf retains the declaration of a complete projected or FromModel result.
// Scalar-only and uninitialized queries fail validation before SQL.
// Example: rows.RecordOf(rows.Select(source,Summary,mappings...)).
func RecordOf[S, R any](q Query[S, R]) Record[R] {
	core := ormcore.RecordSource{Source: q.source, Record: recordDefinition[R](q.recordOwner)}
	core.Source.Err = ormcore.ValidateRecordSource(core, reflect.TypeFor[R](), false)
	return Record[R]{core: core}
}

// ModelRecord adapts a model read without eager loading or write capabilities.
// Example: rows.ModelRecord(Users.Query().Limit(10)).
func ModelRecord[M any](q orm.Query[M]) Record[M] {
	source := orm.FoundrySource(q, sqlowner.Metadata())
	r := ormcore.RecordSource{Source: source, Record: recordDefinition[M](source.Owners[source.Node.Source.Name()]), Direct: ormcore.DirectRecord(source)}
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[M](), false)
	return Record[M]{core: r}
}

func recordDefinition[R any](owner any) ormcore.Record {
	if row, ok := owner.(*rowCore); ok {
		if row == nil || row.typ != reflect.TypeFor[R]() {
			return ormcore.Record{Err: ormcore.Invalid("record requires its row declaration")}
		}
		columns := make([]querycore.Column, len(row.columns))
		for i, c := range row.columns {
			columns[i] = querycore.Column{Name: c.Name, Nullable: c.Nullable}
		}
		return ormcore.Record{Type: row.typ, Owner: row, Columns: columns, Err: row.err, Checks: []func() error{row.check}, Scan: func(r database.Row, p unsafe.Pointer) error { return scanDeclaredRow(row, r, p) }}
	}
	return orm.FoundryRecord[R](ormcore.Scope{Reference: ormcore.Reference{Owner: owner}}, sqlowner.Metadata())
}

func recordOf[R any](source RecordSource[R]) ormcore.RecordSource {
	if ormcore.NilDescriptor(source) {
		return ormcore.RecordSource{Source: ormcore.Source{Err: ormcore.Invalid("record requires a source")}}
	}
	r := source.recordData(nil)
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[R](), false)
	return r
}

// AsRecord aliases a complete model/projection while retaining its SELECT boundary.
// Example: rows.AsRecord[summaryTag](rows.RecordOf(report),"summary").
func AsRecord[A, R any](source RecordSource[R], alias string) Aliased[A, R] {
	s := ormcore.AliasRecord(recordOf(source), alias, reflect.TypeFor[R](), reflect.TypeFor[Alias[A, R]](), false)
	return Aliased[A, R]{source: s, scope: ormcore.Scope{Reference: ormcore.Reference{Table: alias, Owner: s.Owners[alias]}, Err: s.Err}}
}

// RecordRead retains the input scope and complete record decoder without model writes.
// Example: rows.SelectRecord(alias,rows.Scope(alias)).All(ctx,db).
type RecordRead[S, R any] struct {
	_            [0]*S
	_            [0]*R
	ordinaryRead [0]struct{}
	core         ormcore.RecordSource
}

func (r RecordRead[S, R]) recordData(*R) ormcore.RecordSource { return r.core }

// SelectRecord selects a complete preserved source through its original decoder.
// Nullable sides require an explicit declared projection instead.
// Example: rows.SelectRecord(joined,rows.LeftScope(joined,rows.Scope(parent))).
func SelectRecord[S, R any](source SelectionSource[S], scope expr.Scope[S, R]) RecordRead[S, R] {
	s := expr.FoundryScopeData(scope, sqlowner.Expressions())
	return RecordRead[S, R]{core: ormcore.SelectRecord(sourceOf(source), s, recordDefinition[R](s.Owner), reflect.TypeFor[R]())}
}

// Where retains predicates in the input scope. Example: result.Where(id.Gt(0)).
func (r RecordRead[S, R]) Where(ps ...expr.Predicate[S]) RecordRead[S, R] {
	r.core.Source = whereSource(r.core.Source, ps)
	r.core.Direct = false
	return r
}

// OrderBy retains row or selected ordering. Example: result.OrderBy(id.Asc()).
func (r RecordRead[S, R]) OrderBy(os ...expr.ProjectionOrder[S]) RecordRead[S, R] {
	r.core.Source = recordOrders(r.core.Source, os)
	r.core.Direct = false
	return r
}

// Limit retains the result window. Example: result.Limit(10).
func (r RecordRead[S, R]) Limit(n int) RecordRead[S, R] {
	r.core.Source.Node.Limit = value.Set(n)
	r.core.Direct = false
	return r
}

// Offset retains the result offset. Example: result.Offset(10).
func (r RecordRead[S, R]) Offset(n int) RecordRead[S, R] {
	r.core.Source.Node.Offset = n
	r.core.Direct = false
	return r
}

// All returns complete results after closing rows and dispatching model retrieval.
// Example: result.All(ctx,db).
func (r RecordRead[S, R]) All(ctx context.Context, ex database.Executor) ([]R, error) {
	return readRecord[R](ctx, ex, r.core, false)
}

func recordOrders[S any](source ormcore.Source, os []expr.ProjectionOrder[S]) ormcore.Source {
	values := make([]ormcore.Order, len(os))
	for i, o := range os {
		values[i] = expr.FoundryProjectionOrder(o, sqlowner.Expressions())
	}
	return ormcore.OrderBy(source, values)
}
func readRecord[R any](ctx context.Context, ex database.Executor, core ormcore.RecordSource, transaction bool) ([]R, error) {
	items := make([]R, 0)
	err := ormcore.ReadRecord(ctx, ex, core, reflect.TypeFor[R](), transaction, func(row database.Row) error {
		var item R
		if core.Record.Scan == nil {
			return ormcore.Invalid("record requires a decoder")
		}
		if err := core.Record.Scan(row, unsafe.Pointer(&item)); err != nil {
			return err
		}
		items = append(items, item)
		return nil
	}, func() (unsafe.Pointer, int) { return unsafe.Pointer(unsafe.SliceData(items)), len(items) })
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Limit retains an ordinary projection's window without adding Query methods.
// Example: rows.Limit(report,10).
func Limit[S, R any](q Query[S, R], n int) Query[S, R] { q.source.Node.Limit = value.Set(n); return q }

// Offset retains an ordinary projection's offset without adding Query methods.
// Example: rows.Offset(report,10).
func Offset[S, R any](q Query[S, R], n int) Query[S, R] { q.source.Node.Offset = n; return q }
