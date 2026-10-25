package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

type protectedConnector struct {
	source        driver.Connector
	registerTypes func(context.Context, *TypeRegistry) error
}

func (c protectedConnector) Driver() driver.Driver { return c.source.Driver() }
func (c protectedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	source, err := c.source.Connect(ctx)
	if err != nil {
		return nil, err
	}
	native := source.(*stdlib.Conn)
	if err := initializeTypeRegistry(ctx, native.Conn(), c.registerTypes); err != nil {
		discardRewriterConnection(ctx, native.Conn())
		return nil, err
	}
	return &protectedConnection{Conn: native}, nil
}

type protectedConnection struct{ *stdlib.Conn }

func (c *protectedConnection) CheckNamedValue(v *driver.NamedValue) error {
	return c.Conn.CheckNamedValue(v)
}
func (c *protectedConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.ExecContext(ctx, q, c.arguments(args))
}
func (c *protectedConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.Conn.QueryContext(ctx, q, c.arguments(args))
	if rows == nil {
		return nil, err
	}
	return &protectedRows{Rows: rows.(*stdlib.Rows)}, err
}
func (c *protectedConnection) arguments(args []driver.NamedValue) []driver.NamedValue {
	copied := append([]driver.NamedValue(nil), args...)
	prefix := true
	for i := range copied {
		value := copied[i].Value
		if prefix {
			switch v := value.(type) {
			case pgx.QueryExecMode, pgx.QueryResultFormats, pgx.QueryResultFormatsByOID:
				continue
			case pgx.QueryRewriter:
				copied[i].Value = protectedRewriter{source: v, types: c.Conn.Conn().TypeMap()}
				continue
			}
			prefix = false
		}
		copied[i].Value = protectValue(c.Conn.Conn().TypeMap(), value)
	}
	return copied
}

type protectedRewriter struct {
	source pgx.QueryRewriter
	types  *pgtype.Map
}

func (r protectedRewriter) RewriteQuery(ctx context.Context, conn *pgx.Conn, query string, args []any) (result string, values []any, err error) {
	custom := true
	switch r.source.(type) {
	case pgx.NamedArgs, pgx.StrictNamedArgs:
		custom = false
	}
	var before *registrationPlan
	if custom {
		before, err = registrySentinel(r.types)
		if err != nil {
			discardRewriterConnection(ctx, conn)
			return "", nil, err
		}
	}
	original := make([]any, len(args))
	for i, v := range args {
		if original, ok := inputValue(v); ok {
			v = original
		}
		original[i] = v
	}
	err = sqlcallback.Invoke("rewrite PostgreSQL query", func() error {
		var e error
		result, values, e = r.source.RewriteQuery(ctx, conn, query, original)
		return e
	})
	if err != nil {
		if custom {
			discardRewriterConnection(ctx, conn)
		}
		return "", nil, err
	}
	if custom {
		after, failure := registrySentinel(r.types)
		if failure != nil || after != before {
			discardRewriterConnection(ctx, conn)
			return "", nil, registrationFailure()
		}
	}
	err = sqlcallback.Invoke("protect rewritten PostgreSQL arguments", func() error {
		values = append([]any(nil), values...)
		for i, v := range values {
			values[i] = protectValue(conn.TypeMap(), v)
		}
		return nil
	})
	if err != nil {
		discardRewriterConnection(ctx, conn)
		return "", nil, err
	}
	return
}

type protectedRows struct{ *stdlib.Rows }

func (r *protectedRows) ScanColumn(ctx driver.ScanContext, index int, dest any) error {
	if !sqlDestination(reflect.TypeOf(dest)) {
		dest = protectTarget(dest)
	}
	return sqlcallback.Invoke("scan PostgreSQL column", func() error { return r.Rows.ScanColumn(ctx, index, dest) })
}

// Keep the driver's ordinary database/sql scalar and Scanner conversion rules.
func sqlDestination(t reflect.Type) bool {
	for n := 0; t != nil && n < 64; n++ {
		if t.Implements(reflect.TypeFor[sql.Scanner]()) {
			return true
		}
		if t.Implements(reflect.TypeFor[pgtype.ArraySetter]()) || t.Implements(reflect.TypeFor[pgtype.BytesScanner]()) {
			return false
		}
		if t.Kind() != reflect.Pointer {
			break
		}
		t = t.Elem()
	}
	if t == nil {
		return true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.String, reflect.Interface:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Struct:
		return t.ConvertibleTo(reflect.TypeFor[time.Time]())
	}
	return false
}

func (c *protectedConnection) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *protectedConnection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.Conn.PrepareContext(ctx, query)
	if stmt == nil {
		return nil, err
	}
	return protectedStatement{Stmt: stmt.(*stdlib.Stmt), owner: c}, err
}

type protectedStatement struct {
	*stdlib.Stmt
	owner *protectedConnection
}

func (s protectedStatement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.Stmt.ExecContext(ctx, s.owner.arguments(args))
}
func (s protectedStatement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := s.Stmt.QueryContext(ctx, s.owner.arguments(args))
	if rows == nil {
		return nil, err
	}
	return &protectedRows{Rows: rows.(*stdlib.Rows)}, err
}
