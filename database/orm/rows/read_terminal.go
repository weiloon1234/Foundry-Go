package rows

import (
	"context"
	"database/sql/driver"
	"reflect"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// CompileRead inspects an ordinary scalar, record or set read without I/O.
// Example: rows.CompileRead(rows.ReadScalar(selected)).
func CompileRead[S, R any](q ReadSource[S, R]) (orm.Statement, error) {
	return compileReadContext(nil, readSourceOf(q))
}

// PreflightRead validates an ordinary read using the executing request context.
// Example: rows.PreflightRead(ctx, rows.ReadSet(combined)).
func PreflightRead[S, R any](ctx context.Context, q ReadSource[S, R]) error {
	if ctx == nil {
		return ormcore.Invalid("query execution requires a context")
	}
	_, err := compileReadContext(ctx, readSourceOf(q))
	return err
}

func compileReadContext(ctx context.Context, r ormcore.RecordSource) (orm.Statement, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return orm.Statement{}, err
		}
	}
	if r.Transaction {
		return orm.Statement{}, ormcore.Invalid("ordinary read requires an ordinary source")
	}
	s, err := ormcore.CompileRecord(ctx, r)
	if err != nil {
		return orm.Statement{}, err
	}
	return orm.FoundryStatement(s, sqlowner.Metadata())
}

// EachRead iterates an ordinary scalar, record or set with bounded memory.
// Complete models with possible retrieval callbacks use DefaultChunkSize batches,
// closing rows before dispatch and delivery. Other results stream one row at a
// time. Ordering/windows and callback error identity are preserved; batches do
// not share a snapshot. Example: rows.EachRead(ctx, db, selectedRecord, visit).
func EachRead[S, R any](ctx context.Context, ex database.Executor, q ReadSource[S, R], yield func(R) error) error {
	return eachRead(ctx, ex, readSourceOf(q), yield)
}

func eachRead[R any](ctx context.Context, ex database.Executor, r ormcore.RecordSource, yield func(R) error) error {
	if err := ormcore.RecordExecution(ctx, ex, false); err != nil {
		return err
	}
	if yield == nil {
		return ormcore.Invalid("query iteration requires a callback")
	}
	if err := ormcore.ValidateRecordSource(r, reflect.TypeFor[R](), false); err != nil {
		return err
	}
	if !ormcore.RecordMayRetrieve(r.Record, ex) {
		source := r.Source
		source.Checks = ormcore.AppendChecks(source.Checks, r.Record.Checks...)
		// Reuse the DTO stream owner for row closure and isolated delivery.
		return Each(ctx, ex, Query[R, R]{source: source, decode: func(row database.Row) (R, error) {
			var item R
			err := r.Record.Scan(row, unsafe.Pointer(&item))
			return item, err
		}}, yield)
	}
	index := -1
	for i, column := range r.Record.Columns {
		if column.Name == r.Record.Primary {
			index = i
			break
		}
	}
	var items []R
	var key func() (driver.Value, error)
	if r.Record.Fields != nil && index >= 0 {
		fields := r.Record.Fields()
		if index < len(fields) && fields[index].Name == r.Record.Primary && fields[index].Get != nil {
			get := fields[index].Get
			key = func() (driver.Value, error) { return get(unsafe.Pointer(&items[len(items)-1])) }
		}
	}
	return ormcore.RecordBatches(ctx, r.Source.Node, index, func(node querycore.SelectNode) error {
		r.Source.Node = node
		_, err := compileReadContext(ctx, r)
		return err
	}, func(node querycore.SelectNode) (int, error) {
		r.Source.Node = node
		var err error
		items, err = readRecord[R](ctx, ex, r, false)
		return len(items), err
	}, key, func(i int) error { return yield(items[i]) })
}
