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
	"github.com/weiloon1234/Foundry-Go/value"
)

// ReadSource carries scope, complete decoding and executor kind together for locking.
// Example: rows.ForUpdate(rows.Read(report)).
type ReadSource[S, R any] interface {
	readSource(*S, *R) ormcore.RecordSource
}

// Result exposes ordinary read terminals without adding Query methods.
// Example: rows.Read(report).Paginate(ctx,db,orm.Page(1,20)).
type Result[S, R any] struct {
	_              [0]*S
	_              [0]*R
	ordinaryResult [0]struct{}
	core           ormcore.RecordSource
}

func (r Result[S, R]) readSource(*S, *R) ormcore.RecordSource          { return r.core }
func (r RecordRead[S, R]) readSource(*S, *R) ormcore.RecordSource      { return r.core }
func (r TransactionRead[S, R]) readSource(*S, *R) ormcore.RecordSource { return r.core }

// Read adapts complete projections and single-value queries for ordinary terminals.
// Example: rows.Read(rows.SelectValue(Users.Query(),Users.Name)).First(ctx,db).
func Read[S, R any](q Query[S, R]) Result[S, R] {
	if q.recordOwner != nil {
		return Result[S, R]{core: RecordOf(q).core}
	}
	owner := new(struct{ scalar bool })
	r := ormcore.RecordSource{Source: q.source, Record: ormcore.Record{Type: reflect.TypeFor[R](), Owner: owner, Columns: []querycore.Column{{Name: "value", Nullable: value.IsNullableType[R]()}}}}
	if q.decode != nil {
		r.Record.Scan = func(row database.Row, p unsafe.Pointer) error {
			item, err := q.decode(row)
			if err == nil {
				*(*R)(p) = item
			}
			return err
		}
	}
	r.Source.Err = ormcore.ValidateRecordSource(r, reflect.TypeFor[R](), false)
	return Result[S, R]{core: r}
}

// ReadScalar adapts a selected scalar with its exact codec. Example: rows.ReadScalar(selected).All(ctx,db).
func ReadScalar[S, V any](q Scalar[S, V]) Result[S, V] { return Result[S, V]{core: Values(q).core} }

// ReadRecord adapts complete selected records for ordinary terminals. Example: rows.ReadRecord(selected).First(ctx,db).
func ReadRecord[S, R any](q RecordRead[S, R]) Result[S, R] { return Result[S, R]{core: q.core} }

// ReadSet adapts the completed scope of a record set. Example: rows.ReadSet(combined).Count(ctx,db).
func ReadSet[R any](q SetQuery[R]) Result[Set[R], R] {
	return Result[Set[R], R]{core: ormcore.SetRecord(q.core)}
}

// ReadValues adapts the completed scope of a scalar set. Example: rows.ReadValues(combined).SimplePaginate(ctx,db,orm.Page(1,20)).
func ReadValues[V any](q ValueSetQuery[V]) Result[Set[V], V] {
	return Result[Set[V], V]{core: ormcore.SetRecord(q.core)}
}

// SelectTransactionValue retains exact scalar codecs and the transaction requirement.
// Example: rows.SelectTransactionValue(source,expr.Row(id).Value()).All(ctx,tx).
func SelectTransactionValue[S, V any](source TransactionSelectionSource[S], selected expr.Selected[S, V]) TransactionValue[S, V] {
	q := SelectScalar(selectionAdapter[S]{source: transactionSelectionOf(source)}, selected)
	r := Values(q).core
	r.Transaction = true
	return TransactionValue[S, V]{core: r, column: q.column}
}

// TransactionValue is a scalar result executable only through a transaction.
// Example: selected.First(ctx,tx).
type TransactionValue[S, V any] struct {
	_                     [0]*S
	_                     [0]*V
	transactionValuePhase [0]struct{}
	column                ormcore.Column[V]
	core                  ormcore.RecordSource
}

func (q TransactionValue[S, V]) readSource(*S, *V) ormcore.RecordSource { return q.core }

// Where filters the retained input scope. Example: selected.Where(id.Ne(zero)).
func (q Result[S, R]) Where(ps ...expr.Predicate[S]) Result[S, R] {
	q.core.Source = whereSource(q.core.Source, ps)
	q.core.Direct = false
	return q
}

// OrderBy retains explicit result ordering. Example: selected.OrderBy(id.Asc()).
func (q Result[S, R]) OrderBy(os ...expr.ProjectionOrder[S]) Result[S, R] {
	q.core.Source = recordOrders(q.core.Source, os)
	q.core.Direct = false
	return q
}

// Limit bounds selected rows. Example: selected.Limit(10).
func (q Result[S, R]) Limit(n int) Result[S, R] {
	q.core.Source.Node.Limit = value.Set(n)
	q.core.Direct = false
	return q
}

// Offset skips selected rows. Example: selected.Offset(10).
func (q Result[S, R]) Offset(n int) Result[S, R] {
	q.core.Source.Node.Offset = n
	q.core.Direct = false
	return q
}

// All collects complete results or discards the entire batch. Example: selected.All(ctx,executor).
func (q Result[S, R]) All(ctx context.Context, ex database.Executor) ([]R, error) {
	return readRecord[R](ctx, ex, q.core, false)
}

// First returns an omitted optional for an empty result. Example: selected.First(ctx,executor).
func (q Result[S, R]) First(ctx context.Context, ex database.Executor) (value.Optional[R], error) {
	return firstRecord[R](ctx, ex, q.core, false)
}

// RequireFirst reports database.NotFound for an empty result. Example: selected.RequireFirst(ctx,executor).
func (q Result[S, R]) RequireFirst(ctx context.Context, ex database.Executor) (R, error) {
	return requireRecord[R](ctx, ex, q.core, false)
}

// Count counts the selected window, preserving nested locks. Example: selected.Count(ctx,executor).
func (q Result[S, R]) Count(ctx context.Context, ex database.Executor) (int64, error) {
	return ormcore.RecordCount(ctx, ex, q.core)
}

// Exists tests the selected window. Example: selected.Exists(ctx,executor).
func (q Result[S, R]) Exists(ctx context.Context, ex database.Executor) (bool, error) {
	return ormcore.RecordExists(ctx, ex, q.core)
}

// Where filters the retained input scope. Example: selected.Where(id.Ne(zero)).
func (q TransactionValue[S, V]) Where(ps ...expr.Predicate[S]) TransactionValue[S, V] {
	q.core.Source = whereSource(q.core.Source, ps)
	q.core.Direct = false
	return q
}

// OrderBy retains explicit result ordering. Example: selected.OrderBy(id.Asc()).
func (q TransactionValue[S, V]) OrderBy(os ...expr.ProjectionOrder[S]) TransactionValue[S, V] {
	q.core.Source = recordOrders(q.core.Source, os)
	q.core.Direct = false
	return q
}

// Limit bounds selected rows. Example: selected.Limit(10).
func (q TransactionValue[S, V]) Limit(n int) TransactionValue[S, V] {
	q.core.Source.Node.Limit = value.Set(n)
	q.core.Direct = false
	return q
}

// Offset skips selected rows. Example: selected.Offset(10).
func (q TransactionValue[S, V]) Offset(n int) TransactionValue[S, V] {
	q.core.Source.Node.Offset = n
	q.core.Direct = false
	return q
}

// All collects complete results or discards the entire batch. Example: selected.All(ctx,executor).
func (q TransactionValue[S, V]) All(ctx context.Context, ex *database.Tx) ([]V, error) {
	return readRecord[V](ctx, ex, q.core, true)
}

// First returns an omitted optional for an empty result. Example: selected.First(ctx,executor).
func (q TransactionValue[S, V]) First(ctx context.Context, ex *database.Tx) (value.Optional[V], error) {
	return firstRecord[V](ctx, ex, q.core, true)
}

// RequireFirst reports database.NotFound for an empty result. Example: selected.RequireFirst(ctx,executor).
func (q TransactionValue[S, V]) RequireFirst(ctx context.Context, ex *database.Tx) (V, error) {
	return requireRecord[V](ctx, ex, q.core, true)
}

// Count counts the selected window, preserving nested locks. Example: selected.Count(ctx,executor).
func (q TransactionValue[S, V]) Count(ctx context.Context, ex *database.Tx) (int64, error) {
	return ormcore.RecordCount(ctx, ex, q.core)
}

// Exists tests the selected window. Example: selected.Exists(ctx,executor).
func (q TransactionValue[S, V]) Exists(ctx context.Context, ex *database.Tx) (bool, error) {
	return ormcore.RecordExists(ctx, ex, q.core)
}

// First returns an omitted optional for an empty result. Example: selected.First(ctx,executor).
func (q TransactionRead[S, R]) First(ctx context.Context, ex *database.Tx) (value.Optional[R], error) {
	return firstRecord[R](ctx, ex, q.core, true)
}

// RequireFirst reports database.NotFound for an empty result. Example: selected.RequireFirst(ctx,executor).
func (q TransactionRead[S, R]) RequireFirst(ctx context.Context, ex *database.Tx) (R, error) {
	return requireRecord[R](ctx, ex, q.core, true)
}

// Count counts the selected window, preserving nested locks. Example: selected.Count(ctx,executor).
func (q TransactionRead[S, R]) Count(ctx context.Context, ex *database.Tx) (int64, error) {
	return ormcore.RecordCount(ctx, ex, q.core)
}

// Exists tests the selected window. Example: selected.Exists(ctx,executor).
func (q TransactionRead[S, R]) Exists(ctx context.Context, ex *database.Tx) (bool, error) {
	return ormcore.RecordExists(ctx, ex, q.core)
}

func firstRecord[R any](ctx context.Context, ex database.Executor, r ormcore.RecordSource, transaction bool) (value.Optional[R], error) {
	items, err := readRecord[R](ctx, ex, ormcore.FirstRecord(r), transaction)
	if err != nil {
		return value.Optional[R]{}, err
	}
	if len(items) == 0 {
		return value.Optional[R]{}, nil
	}
	return value.Set(items[0]), nil
}
func requireRecord[R any](ctx context.Context, ex database.Executor, r ormcore.RecordSource, transaction bool) (R, error) {
	item, err := firstRecord[R](ctx, ex, r, transaction)
	if err != nil {
		return *new(R), err
	}
	if result, ok := item.Get(); ok {
		return result, nil
	}
	return *new(R), database.NewError("query first", database.NotFound)
}

// Paginate requires explicit ordering and no outer window; nested windows remain.
// Example: rows.Read(report).Paginate(ctx,db,orm.Page(1,20)).
func (q Result[S, R]) Paginate(ctx context.Context, ex database.Executor, request orm.PageRequest) (orm.PageResult[R], error) {
	window, err := recordPage(ctx, ex, q.core, request, false)
	if err != nil {
		return orm.PageResult[R]{}, err
	}
	total, err := ormcore.RecordCount(ctx, ex, q.core)
	if err != nil {
		return orm.PageResult[R]{}, err
	}
	if total < 0 {
		return orm.PageResult[R]{}, ormcore.Invalid("database returned a negative page count")
	}
	items, err := readRecord[R](ctx, ex, window, false)
	if err != nil {
		return orm.PageResult[R]{}, err
	}
	pages := querycore.PageCount(total, request.Size)
	return orm.PageResult[R]{Items: items, Number: request.Number, Size: request.Size, Total: total, Pages: pages}, nil
}

// SimplePaginate reads one lookahead row without a total. Example: rows.Read(report).SimplePaginate(ctx,db,orm.Page(1,20)).
func (q Result[S, R]) SimplePaginate(ctx context.Context, ex database.Executor, request orm.PageRequest) (orm.SimplePage[R], error) {
	window, err := recordPage(ctx, ex, q.core, request, true)
	if err != nil {
		return orm.SimplePage[R]{}, err
	}
	items, err := readRecord[R](ctx, ex, window, false)
	if err != nil {
		return orm.SimplePage[R]{}, err
	}
	more := len(items) > request.Size
	if more {
		clear(items[request.Size:])
		items = items[:request.Size:request.Size]
	}
	return orm.SimplePage[R]{Items: items, Number: request.Number, Size: request.Size, HasMore: more}, nil
}
func recordPage(ctx context.Context, ex database.Executor, r ormcore.RecordSource, request orm.PageRequest, lookahead bool) (ormcore.RecordSource, error) {
	if err := ormcore.RecordExecution(ctx, ex, false); err != nil {
		return r, err
	}
	offset, err := querycore.PageOffset(request)
	if err != nil {
		return r, err
	}
	if r.Transaction {
		return r, ormcore.Invalid("result pagination requires explicit ordering and no outer Limit/Offset")
	}
	node, err := querycore.ResultPageWindow(r.Source.Node, offset, request.Size, lookahead)
	if err != nil {
		return r, err
	}
	r.Source.Node = node
	_, err = ormcore.CompileRecord(ctx, r)
	return r, err
}
