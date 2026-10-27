package sqlcallback

import (
	"context"
	"database/sql/driver"
	"io"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Connector shields database/sql's native error inspection from driver callbacks.
func Connector(source driver.Connector) driver.Connector { return connector{source} }

type connector struct{ source driver.Connector }

func (c connector) Driver() driver.Driver { return c.source.Driver() }
func (c connector) Connect(ctx context.Context) (result driver.Conn, err error) {
	err = call("connect SQL driver", func() error { var e error; result, e = c.source.Connect(driverContext(ctx)); return e })
	if result != nil {
		result = &connection{source: result}
	}
	return
}
func call(operation string, fn func() error) error {
	var original error
	contained := callback.Isolated(operation, func() error { original = fn(); return nil })
	return driverError(operation, original, contained)
}

func driverError(operation string, original, contained error) error {
	if contained != nil {
		return safeerror.Seal(operation+" failed", contained)
	}
	if original == nil {
		return nil
	}
	switch original {
	case driver.ErrSkip, driver.ErrBadConn, io.EOF, driver.ErrRemoveArgument:
		return original
	}
	return inspectDriverError(operation, original)
}

func inspectDriverError(operation string, original error) error {
	// Only ErrBadConn uses native errors.Is matching. Skip/remove/EOF use
	// equality in database/sql; wrapping any of those must remain a failure.
	retry := false
	contained := callback.Isolated("inspect SQL driver retry", func() error { retry = errorgraph.Is(original, driver.ErrBadConn); return nil })
	original = Retain(original, original)
	if contained == nil && retry {
		return safeerror.SealMatching(operation+" failed", original, driver.ErrBadConn)
	}

	return safeerror.Seal(operation+" failed", original)
}

type connection struct{ source driver.Conn }

func (c *connection) Prepare(query string) (result driver.Stmt, err error) {
	err = call("prepare SQL", func() error { var e error; result, e = c.source.Prepare(query); return e })
	if result != nil {
		result = &statement{source: result, check: c.CheckNamedValue}
	}
	return
}
func (c *connection) PrepareContext(ctx context.Context, q string) (result driver.Stmt, err error) {
	if s, ok := c.source.(driver.ConnPrepareContext); ok {
		err = call("prepare SQL", func() error { var e error; result, e = s.PrepareContext(driverContext(ctx), q); return e })
		if result != nil {
			result = &statement{source: result, check: c.CheckNamedValue}
		}
		return
	}
	result, err = c.Prepare(q)
	if err == nil {
		select {
		case <-ctx.Done():
			_ = result.Close()
			return nil, ctx.Err()
		default:
		}
	}
	return
}
func (c *connection) Close() error { return call("close SQL connection", c.source.Close) }
func (c *connection) Begin() (result driver.Tx, err error) {
	err = call("begin SQL transaction", func() error { var e error; result, e = c.source.Begin(); return e })
	if result != nil {
		result = transaction{result}
	}
	return
}
func (c *connection) BeginTx(ctx context.Context, opts driver.TxOptions) (result driver.Tx, err error) {
	if s, ok := c.source.(driver.ConnBeginTx); ok {
		err = call("begin SQL transaction", func() error { var e error; result, e = s.BeginTx(driverContext(ctx), opts); return e })
		if result != nil {
			result = transaction{result}
		}
		return
	}
	if opts.Isolation != 0 || opts.ReadOnly {
		return nil, safeerror.Seal("unsupported transaction options", driver.ErrSkip)
	}
	result, err = c.Begin()
	if err == nil {
		select {
		case <-ctx.Done():
			_ = result.Rollback()
			return nil, ctx.Err()
		default:
		}
	}
	return
}
func (c *connection) CheckNamedValue(value *driver.NamedValue) error {
	if s, ok := c.source.(driver.NamedValueChecker); ok {
		err := call("check SQL argument", func() error { return s.CheckNamedValue(value) })
		if err != driver.ErrSkip {
			return err
		}
	}
	value.Value = Argument(value.Value)
	return driver.ErrSkip
}
func (c *connection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (result driver.Result, err error) {
	if s, ok := c.source.(driver.ExecerContext); ok {
		err = call("execute SQL driver", func() error { var e error; result, e = s.ExecContext(driverContext(ctx), q, args); return e })
	} else if s, ok := c.source.(driver.Execer); ok {
		values, e := unnamed(args)
		if e != nil {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		err = call("execute SQL driver", func() error { var e error; result, e = s.Exec(q, values); return e })
	} else {
		return nil, driver.ErrSkip
	}
	if result != nil {
		result = resultValue{result}
	}
	return
}
func (c *connection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (result driver.Rows, err error) {
	batch := rowBatch(ctx)
	ctx = driverContext(ctx)
	if s, ok := c.source.(driver.QueryerContext); ok {
		err = call("query SQL driver", func() error { var e error; result, e = s.QueryContext(ctx, q, args); return e })
	} else if s, ok := c.source.(driver.Queryer); ok {
		values, e := unnamed(args)
		if e != nil {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		err = call("query SQL driver", func() error { var e error; result, e = s.Query(q, values); return e })
	} else {
		return nil, driver.ErrSkip
	}
	if result != nil {
		result = protectRows(result, batch)
	}
	return
}
func (c *connection) Ping(ctx context.Context) error {
	if s, ok := c.source.(driver.Pinger); ok {
		return call("ping SQL driver", func() error { return s.Ping(driverContext(ctx)) })
	}
	return nil
}
func (c *connection) ResetSession(ctx context.Context) error {
	if s, ok := c.source.(driver.SessionResetter); ok {
		return call("reset SQL session", func() error { return s.ResetSession(driverContext(ctx)) })
	}
	return nil
}
func (c *connection) IsValid() bool {
	if s, ok := c.source.(driver.Validator); ok {
		valid := false
		err := callback.Isolated("validate SQL connection", func() error { valid = s.IsValid(); return nil })
		return err == nil && valid
	}
	return true
}

type transaction struct{ source driver.Tx }

func (t transaction) Commit() error   { return call("commit SQL transaction", t.source.Commit) }
func (t transaction) Rollback() error { return call("rollback SQL transaction", t.source.Rollback) }

type resultValue struct{ source driver.Result }

func (r resultValue) LastInsertId() (n int64, err error) {
	err = call("read SQL insert ID", func() error { var e error; n, e = r.source.LastInsertId(); return e })
	return
}
func (r resultValue) RowsAffected() (n int64, err error) {
	err = call("read SQL affected rows", func() error { var e error; n, e = r.source.RowsAffected(); return e })
	return
}
func unnamed(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, v := range args {
		if v.Name != "" {
			return nil, safeerror.Seal("named SQL arguments unsupported", driver.ErrSkip)
		}
		values[i] = v.Value
	}
	return values, nil
}

type statement struct {
	source driver.Stmt
	check  func(*driver.NamedValue) error
}

func (s *statement) Close() error  { return call("close SQL statement", s.source.Close) }
func (s *statement) NumInput() int { return s.source.NumInput() }
func (s *statement) Exec(args []driver.Value) (result driver.Result, err error) {
	err = call("execute SQL statement", func() error { var e error; result, e = s.source.Exec(args); return e })
	if result != nil {
		result = resultValue{result}
	}
	return
}
func (s *statement) Query(args []driver.Value) (result driver.Rows, err error) {
	err = call("query SQL statement", func() error { var e error; result, e = s.source.Query(args); return e })
	if result != nil {
		result = protectRows(result)
	}
	return
}
func (s *statement) ExecContext(ctx context.Context, args []driver.NamedValue) (result driver.Result, err error) {
	if native, ok := s.source.(driver.StmtExecContext); ok {
		err = call("execute SQL statement", func() error { var e error; result, e = native.ExecContext(driverContext(ctx), args); return e })
		if result != nil {
			result = resultValue{result}
		}
		return
	}
	values, e := unnamed(args)
	if e != nil {
		return nil, e
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return s.Exec(values)
}
func (s *statement) QueryContext(ctx context.Context, args []driver.NamedValue) (result driver.Rows, err error) {
	if native, ok := s.source.(driver.StmtQueryContext); ok {
		err = call("query SQL statement", func() error { var e error; result, e = native.QueryContext(driverContext(ctx), args); return e })
		if result != nil {
			result = protectRows(result, rowBatch(ctx))
		}
		return
	}
	values, e := unnamed(args)
	if e != nil {
		return nil, e
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return s.Query(values)
}
func (s *statement) CheckNamedValue(value *driver.NamedValue) error {
	if native, ok := s.source.(driver.NamedValueChecker); ok {
		err := call("check SQL statement argument", func() error { return native.CheckNamedValue(value) })
		if err != driver.ErrSkip {
			return err
		}
	} else if s.check != nil {
		return s.check(value)
	}
	value.Value = Argument(value.Value)
	return driver.ErrSkip
}
func (s *statement) ColumnConverter(index int) driver.ValueConverter {
	if native, ok := s.source.(driver.ColumnConverter); ok {
		return converter{native.ColumnConverter(index)}
	}
	return converter{driver.DefaultParameterConverter}
}

type converter struct{ source driver.ValueConverter }

func (c converter) ConvertValue(v any) (value driver.Value, err error) {
	err = call("convert SQL statement argument", func() error { var e error; value, e = c.source.ConvertValue(Argument(v)); return e })
	return
}

type rows struct {
	source driver.Rows
	batch  *RowBatch
}

func protectRows(source driver.Rows, batch ...*RowBatch) driver.Rows {
	base := &rows{source: source}
	if len(batch) != 0 {
		base.batch = batch[0]
	}
	if native, ok := source.(driver.RowsColumnScanner); ok {
		return &columnRows{rows: base, native: native}
	}
	return base
}
func (r *rows) Columns() []string { return r.source.Columns() }
func (r *rows) Close() error      { return call("close SQL rows", r.source.Close) }
func (r *rows) Next(values []driver.Value) error {
	if r.batch != nil && r.batch.active.Load() {
		return r.batch.call("iterate SQL rows", func() error { return r.source.Next(values) })
	}
	return call("iterate SQL rows", func() error { return r.source.Next(values) })
}
func (r *rows) HasNextResultSet() bool {
	if s, ok := r.source.(driver.RowsNextResultSet); ok {
		return s.HasNextResultSet()
	}
	return false
}
func (r *rows) NextResultSet() error {
	if s, ok := r.source.(driver.RowsNextResultSet); ok {
		return call("advance SQL result", s.NextResultSet)
	}
	return io.EOF
}
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	if s, ok := r.source.(driver.RowsColumnTypeScanType); ok {
		return s.ColumnTypeScanType(i)
	}
	return reflect.TypeFor[any]()
}
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	if s, ok := r.source.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return s.ColumnTypeDatabaseTypeName(i)
	}
	return ""
}
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	if s, ok := r.source.(driver.RowsColumnTypeLength); ok {
		return s.ColumnTypeLength(i)
	}
	return 0, false
}
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	if s, ok := r.source.(driver.RowsColumnTypeNullable); ok {
		return s.ColumnTypeNullable(i)
	}
	return false, false
}
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if s, ok := r.source.(driver.RowsColumnTypePrecisionScale); ok {
		return s.ColumnTypePrecisionScale(i)
	}
	return 0, 0, false
}

type columnRows struct {
	*rows
	native driver.RowsColumnScanner
}

func (r *columnRows) NextRow() error {
	if r.batch != nil && r.batch.active.Load() {
		return r.batch.call("iterate SQL native rows", r.native.NextRow)
	}
	return call("iterate SQL native rows", r.native.NextRow)
}
func (r *columnRows) ScanColumn(ctx driver.ScanContext, i int, dst any) error {
	return call("scan SQL native column", func() error { return r.native.ScanColumn(ctx, i, dst) })
}
