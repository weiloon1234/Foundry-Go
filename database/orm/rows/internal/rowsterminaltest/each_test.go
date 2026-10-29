package rowsterminaltest

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"io"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestEachStreamsUntilCallbackFailure(t *testing.T) {
	stop := errors.New("stop iteration")
	closeFailure := errors.New("close failure")
	state := &streamState{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(9), "Ada"}, {"invalid integer", "not decoded"}}, closeErr: closeFailure}
	var got []streamRecord
	err := rows.EachRead(t.Context(), openStreamDB(t, state), rows.Read(streamProjection()), func(row streamRecord) error {
		if state.closed.Load() {
			t.Error("callback must run while rows are open")
		}
		got = append(got, row)
		return stop
	})
	if !errors.Is(err, stop) || !errors.Is(err, closeFailure) || len(got) != 1 || got[0] != (streamRecord{9, "Ada"}) || !state.closed.Load() || state.queries != 1 {
		t.Fatalf("stream: rows=%v closed=%v statements=%d err=%v", got, state.closed.Load(), state.queries, err)
	}
	want := `SELECT "members"."id" AS "id", "members"."name" AS "name" FROM "members" WHERE ("members"."name" = $1) ORDER BY "members"."id" ASC`
	if state.sql != want {
		t.Fatalf("SQL=%s", state.sql)
	}
}

func TestEachDecodeFailureClosesRows(t *testing.T) {
	state := &streamState{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(9), "Ada"}, {"invalid integer", "Bad"}}}
	yielded := 0
	err := rows.EachRead(t.Context(), openStreamDB(t, state), rows.Read(streamProjection()), func(streamRecord) error { yielded++; return nil })
	if err == nil || yielded != 1 || !state.closed.Load() || state.queries != 1 {
		t.Fatalf("yielded=%d closed=%v statements=%d err=%v", yielded, state.closed.Load(), state.queries, err)
	}
}

func TestEachRejectsNilCallbackBeforeSQL(t *testing.T) {
	state := &streamState{}
	if err := rows.EachRead(t.Context(), openStreamDB(t, state), rows.Read(streamProjection()), nil); !errors.Is(err, fault.Invalid) || state.queries != 0 {
		t.Fatalf("statements=%d err=%v", state.queries, err)
	}
}

func TestEachRetainsPredicatesAndUsesExecutingScopeContext(t *testing.T) {
	type tenantKey struct{}
	table := orm.Define("members", func(b orm.Builder[streamRecord], m *streamRecord) streamTable {
		return streamTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	}, orm.WithGlobalScopes(func(table streamTable) []orm.GlobalScope[streamRecord] {
		return []orm.GlobalScope[streamRecord]{orm.Scope(table.Table, "tenant", func(ctx context.Context) (orm.Predicate[streamRecord], error) {
			id, ok := ctx.Value(tenantKey{}).(int64)
			if !ok {
				return orm.Predicate[streamRecord]{}, errors.New("tenant required")
			}
			return orm.KeyEq(table.Key(), id), nil
		})}
	}))
	bound := context.WithValue(t.Context(), tenantKey{}, int64(1))
	executing := context.WithValue(t.Context(), tenantKey{}, int64(2))
	q := rows.SelectValue(orm.WithScopeContext(table.Query().Where(orm.TextEq(table.Name, "Ada")), bound), orm.KeyExpr(table.Key()))
	state := &streamState{columns: []string{"value"}, values: [][]driver.Value{{int64(2)}}}
	var got int64
	err := rows.EachRead(executing, openStreamDB(t, state), rows.Read(q), func(id int64) error { got = id; return nil })
	if err != nil || got != 2 || !state.closed.Load() || state.queries != 1 {
		t.Fatalf("got=%d closed=%v statements=%d err=%v", got, state.closed.Load(), state.queries, err)
	}
	foundName, foundTenant := false, false
	for _, arg := range state.args {
		foundName = foundName || arg.Value == "Ada"
		foundTenant = foundTenant || arg.Value == int64(2)
		if arg.Value == int64(1) {
			t.Fatal("execution used stale bound scope context")
		}
	}
	if !foundName || !foundTenant {
		t.Fatalf("missing explicit predicate or fresh scope: %v", state.args)
	}
}

func TestEachCancellationAndPanicCloseRows(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		state := &streamState{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(9), "Ada"}, {int64(10), "Next"}}}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		yielded := 0
		err := rows.EachRead(ctx, openStreamDB(t, state), rows.Read(streamProjection()), func(streamRecord) error { yielded++; cancel(); return nil })
		if !errors.Is(err, context.Canceled) || yielded != 1 || !state.closed.Load() || state.queries != 1 {
			t.Fatalf("yielded=%d closed=%v statements=%d err=%v", yielded, state.closed.Load(), state.queries, err)
		}
	})
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			state := &streamState{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(9), "Ada"}}}
			err := rows.EachRead(t.Context(), openStreamDB(t, state), rows.Read(streamProjection()), func(streamRecord) error {
				if mode == "panic" {
					panic("private callback payload")
				}
				runtime.Goexit()
				return nil
			})
			if !errors.Is(err, fault.Panicked) || !state.closed.Load() || state.queries != 1 {
				t.Fatalf("closed=%v statements=%d err=%v", state.closed.Load(), state.queries, err)
			}
		})
	}
}

// All declarations, projection construction and execution use only public APIs.
type streamRecord struct {
	ID   int64
	Name string
}
type streamTable struct {
	orm.Table[streamRecord, int64]
	Name orm.Text[streamRecord, string]
}
type streamFields struct {
	rows.Row[streamRecord]
	ID   rows.Field[streamRecord, int64]
	Name rows.Field[streamRecord, string]
}

func streamProjection() rows.Query[streamRecord, streamRecord] {
	table := orm.Define("members", func(b orm.Builder[streamRecord], m *streamRecord) streamTable {
		return streamTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
	})
	fields := rows.Define(func(b rows.Builder[streamRecord], r *streamRecord) streamFields {
		return streamFields{b.Row(), rows.NumberOf(b, &r.ID), rows.TextOf(b, &r.Name)}
	})
	return rows.Select(rows.From(table.Query().Where(orm.TextEq(table.Name, "Ada")).OrderBy(orm.KeyAsc(table.Key()))), fields, rows.Map(fields.ID, expr.Value(orm.KeyExpr(table.Key()))), rows.Map(fields.Name, expr.Value(orm.TextExpr(table.Name))))
}

type streamState struct {
	beforeQuery func(string)
	sql         string
	args        []driver.NamedValue
	queries     int
	columns     []string
	values      [][]driver.Value
	closed      atomic.Bool
	closeErr    error
}
type streamConnector struct{ state *streamState }

func (c streamConnector) Driver() driver.Driver                        { return c }
func (c streamConnector) Open(string) (driver.Conn, error)             { return &streamConn{c.state}, nil }
func (c streamConnector) Connect(context.Context) (driver.Conn, error) { return c.Open("") }

type streamConn struct{ state *streamState }

func (*streamConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (*streamConn) Close() error                        { return nil }
func (*streamConn) Begin() (driver.Tx, error)           { return nil, errors.New("unexpected transaction") }
func (*streamConn) Ping(context.Context) error          { return nil }
func (c *streamConn) QueryContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	if c.state.beforeQuery != nil {
		c.state.beforeQuery(sql)
	}
	c.state.sql = sql
	c.state.args = append([]driver.NamedValue(nil), args...)
	c.state.queries++
	return &streamDriverRows{state: c.state}, nil
}

type streamDriverRows struct {
	state *streamState
	index int
}

func (r *streamDriverRows) Columns() []string { return r.state.columns }
func (r *streamDriverRows) Close() error      { r.state.closed.Store(true); return r.state.closeErr }
func (r *streamDriverRows) Next(dest []driver.Value) error {
	if r.index >= len(r.state.values) {
		return io.EOF
	}
	copy(dest, r.state.values[r.index])
	r.index++
	return nil
}
func openStreamDB(t *testing.T, state *streamState) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: streamConnector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	return db
}
