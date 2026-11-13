package datatable

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type collationQuery struct {
	statement string
	arguments []driver.NamedValue
	deadline  time.Time
}

type collationProbe struct {
	exists     bool
	err        error
	wait       bool
	queries    atomic.Int32
	closedRows atomic.Int32
	observed   chan collationQuery
	commands   chan string
}

func (p *collationProbe) Driver() driver.Driver                        { return p }
func (p *collationProbe) Open(string) (driver.Conn, error)             { return p, nil }
func (p *collationProbe) Connect(context.Context) (driver.Conn, error) { return p, nil }
func (p *collationProbe) Close() error                                 { return nil }
func (p *collationProbe) Ping(context.Context) error                   { return nil }
func (p *collationProbe) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (p *collationProbe) Begin() (driver.Tx, error) { return collationTransaction{}, nil }
func (p *collationProbe) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return p.Begin()
}
func (p *collationProbe) ExecContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
	p.commands <- statement
	return driver.RowsAffected(0), nil
}
func (p *collationProbe) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	p.queries.Add(1)
	deadline, _ := ctx.Deadline()
	p.observed <- collationQuery{statement, append([]driver.NamedValue(nil), args...), deadline}
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.err != nil {
		return nil, p.err
	}
	return &collationResult{probe: p}, nil
}

type collationTransaction struct{}

func (collationTransaction) Commit() error   { return nil }
func (collationTransaction) Rollback() error { return nil }

type collationResult struct {
	probe *collationProbe
	done  bool
}

func (*collationResult) Columns() []string { return []string{"exists"} }
func (r *collationResult) Close() error    { r.probe.closedRows.Add(1); return nil }
func (r *collationResult) Next(dst []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dst[0] = r.probe.exists
	return nil
}

func collationManager(t *testing.T, config Config, probe *collationProbe) *Manager {
	t.Helper()
	probe.observed, probe.commands = make(chan collationQuery, 4), make(chan string, 4)
	db, err := database.Open(t.Context(), database.Adapter{Connector: probe}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	m, err := New(Dependencies{Database: db}, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestModuleChecksConfiguredCollation(t *testing.T) {
	for _, tc := range []struct {
		name   expr.Collation
		exists bool
		quoted string
	}{
		{"", false, ""}, {expr.CollationC, true, `"C"`},
		{expr.CollationUnicode, true, `"und-x-icu"`},
		{`app"name.with.dot`, true, `"app""name.with.dot"`},
		{"zz-x-icu", false, `"zz-x-icu"`},
	} {
		t.Run(string(tc.name), func(t *testing.T) {
			config := DefaultConfig()
			config.TextCollation, config.Schema = tc.name, "report_schema"
			probe := &collationProbe{exists: tc.exists}
			manager := collationManager(t, config, probe)
			key := foundation.NewKey[*Manager]("collation.reports")
			module := Module("collation.reports", key, nil, func(foundation.Resolver) (*Manager, error) { return manager, nil })
			app, err := foundation.NewBuilder().Register(module).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
			err = app.Start(t.Context())
			missing := tc.name != "" && !tc.exists
			if missing {
				if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "report text collation is not available in the database") {
					t.Fatal("missing collation did not safely fail boot", err)
				}
				if err := app.Shutdown(t.Context()); !errors.Is(err, fault.Invalid) {
					t.Fatal("shutdown lost the boot failure", err)
				}
				select {
				case <-manager.DoneQueries():
				default:
					t.Fatal("failed boot retained query admission")
				}
				select {
				case <-manager.DoneExports():
				default:
					t.Fatal("failed boot retained export admission")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.name == "" {
				if probe.queries.Load() != 0 || len(probe.commands) != 0 {
					t.Fatal("empty collation accessed the database")
				}
				return
			}
			if probe.queries.Load() != 1 || probe.closedRows.Load() != 1 {
				t.Fatal("check must issue one query and close its rows")
			}
			query := <-probe.observed
			if query.statement != "SELECT to_regcollation($1) IS NOT NULL" || len(query.arguments) != 1 || query.arguments[0].Value != tc.quoted {
				t.Fatal("collation resolution differs from compiler quoting", query)
			}
			if query.deadline.IsZero() || query.deadline.After(time.Now().Add(config.Timeout)) {
				t.Fatal("check escaped configured timeout", query.deadline)
			}
			if len(probe.commands) != 1 || <-probe.commands != `SET LOCAL search_path TO "report_schema", pg_temp` {
				t.Fatal("check used a different report search path")
			}
		})
	}
}

func TestCheckCollationPreservesDatabaseErrorsAndCancellation(t *testing.T) {
	private := errors.New("private database detail")
	config := DefaultConfig()
	config.TextCollation = expr.CollationC
	probe := &collationProbe{err: private}
	manager := collationManager(t, config, probe)
	if err := manager.CheckCollation(t.Context()); !errors.Is(err, private) || !errors.Is(err, database.QueryFailed) || strings.Contains(err.Error(), private.Error()) {
		t.Fatal("database classification or redaction lost", err)
	}
	for _, callerBound := range []bool{false, true} {
		config := DefaultConfig()
		config.TextCollation = expr.CollationC
		if !callerBound {
			config.Timeout = 20 * time.Millisecond
		}
		manager := collationManager(t, config, &collationProbe{wait: true})
		ctx := t.Context()
		if callerBound {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
			defer cancel()
		}
		if err := manager.CheckCollation(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("check did not honor its deadline", err)
		}
	}
}
