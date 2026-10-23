package orm_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type auditedAppRow struct {
	ID    int64
	Name  string
	Token string
}
type auditedAppTable struct {
	orm.Table[auditedAppRow, int64]
	Name  orm.Text[auditedAppRow, string]
	Token orm.Text[auditedAppRow, string]
}

func TestAuditRegistrationOwnsRecorderAndRollsBackWithBusinessWrite(t *testing.T) {
	table := orm.Define("audited_app", func(b orm.Builder[auditedAppRow], m *auditedAppRow) auditedAppTable {
		return auditedAppTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name), orm.TextOf(b, &m.Token)}
	})
	pool := foundation.NewKey[*database.DB]("audit.pool")
	recorderKey := foundation.NewKey[*audit.Recorder]("audit.recorder")
	veto := errors.New("abort after audit insertion")
	var previous *audit.Recorder
	for range 2 {
		state := &auditAppState{}
		reject := false
		modules := []foundation.Provider{
			database.Module("audit.pool", pool, func() (database.Adapter, error) { return database.Adapter{Connector: auditAppConnector{state}}, nil }, database.DefaultPoolConfig()),
			foundation.Module{Name: "audit.models", OnRegister: func(r *foundation.Registrar) error {
				if err := audit.Register(r, pool, recorderKey, audit.DefaultConfig(), orm.Auditing(table.Table, orm.AuditPolicy[auditedAppRow]{})); err != nil {
					return err
				}
				return orm.RegisterObserver(r, pool, orm.NewObserver[auditedAppRow]("after.audit"), func(foundation.Resolver) (func() orm.Hooks[auditedAppRow], error) {
					return func() orm.Hooks[auditedAppRow] {
						return orm.Hooks[auditedAppRow]{Created: func(context.Context, *database.Tx, orm.Changes[auditedAppRow]) error {
							if reject {
								return veto
							}
							return nil
						}}
					}, nil
				})
			}},
		}
		settings := application.DefaultSettings()
		settings.HTTP.Enabled = false
		app, err := application.New(settings, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))).Register(modules...).Build(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := app.Shutdown(context.Background()); err != nil {
				t.Error(err)
			}
		})
		recorder, err := foundation.Resolve(app.Services(), recorderKey)
		if err != nil || recorder == previous {
			t.Fatal("recorder is not instance owned", err)
		}
		previous = recorder
		if err = app.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		db, err := foundation.Resolve(app.Services(), pool)
		if err != nil {
			t.Fatal(err)
		}
		for _, fail := range []bool{false, true} {
			reject = fail
			_, err = table.Insert(t.Context(), db, table.Key().Set(7), table.Name.Set("input"), table.Token.Set("private-token"))
			if fail {
				if !errors.Is(err, veto) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if state.models != 1 || state.audits != 1 {
				t.Fatalf("transaction escaped rollback: models=%d audits=%d", state.models, state.audits)
			}
		}
		if state.auditAttempts != 2 || state.rollbacks == 0 {
			t.Fatal("failure happened before audit insert", state.auditAttempts, state.rollbacks)
		}
		for _, payload := range state.payloads {
			if strings.Contains(payload, "private-token") || !strings.Contains(payload, "stored-name") {
				t.Fatal("audit did not capture redacted RETURNING data", payload)
			}
		}
	}
}

type auditAppState struct {
	models, audits, pendingModels, pendingAudits, auditAttempts, rollbacks int
	payloads                                                               []string
}
type auditAppConnector struct{ state *auditAppState }

func (c auditAppConnector) Driver() driver.Driver { return c }
func (c auditAppConnector) Open(string) (driver.Conn, error) {
	return &auditAppConnection{c.state}, nil
}
func (c auditAppConnector) Connect(context.Context) (driver.Conn, error) { return c.Open("") }

type auditAppConnection struct{ state *auditAppState }

func (*auditAppConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*auditAppConnection) Close() error               { return nil }
func (*auditAppConnection) Ping(context.Context) error { return nil }
func (c *auditAppConnection) Begin() (driver.Tx, error) {
	c.state.pendingModels = 0
	c.state.pendingAudits = 0
	return auditAppTransaction{c.state}, nil
}
func (c *auditAppConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *auditAppConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (c *auditAppConnection) QueryContext(_ context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(sql, "INSERT INTO") {
		return nil, errors.New("unexpected audit test SQL")
	}
	start, end := strings.Index(sql, "("), strings.Index(sql, ") VALUES")
	if start < 0 || end < start {
		return nil, errors.New("unexpected insert shape")
	}
	names := strings.Split(sql[start+1:end], ",")
	values := map[string]driver.Value{"sequence": int64(1), "created_at": time.Now().UTC()}
	for i, name := range names {
		values[strings.Trim(strings.TrimSpace(name), `"`)] = args[i].Value
	}
	var columns []string
	if strings.Contains(sql, `"foundry_audit"`) {
		c.state.pendingAudits++
		c.state.auditAttempts++
		c.state.payloads = append(c.state.payloads, values["payload"].(string))
		columns = []string{"id", "sequence", "area", "operation", "action", "version", "subject", "subject_key", "payload", "redacted", "redaction", "origin", "actor_key", "correlation", "request_method", "request_route", "created_at"}
	} else {
		c.state.pendingModels++
		values["name"] = "stored-name"
		columns = []string{"id", "name", "token"}
	}
	row := make([]driver.Value, len(columns))
	for i, col := range columns {
		row[i] = values[col]
	}
	return &auditAppRows{columns: columns, values: row}, nil
}

type auditAppTransaction struct{ state *auditAppState }

func (t auditAppTransaction) Commit() error {
	t.state.models += t.state.pendingModels
	t.state.audits += t.state.pendingAudits
	return nil
}
func (t auditAppTransaction) Rollback() error {
	t.state.pendingModels = 0
	t.state.pendingAudits = 0
	t.state.rollbacks++
	return nil
}

type auditAppRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (r *auditAppRows) Columns() []string { return r.columns }
func (*auditAppRows) Close() error        { return nil }
func (r *auditAppRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}
