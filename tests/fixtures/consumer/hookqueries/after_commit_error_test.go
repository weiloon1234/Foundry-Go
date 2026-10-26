package hookqueries_test

import "time"

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"foundry.test/consumer/hookqueries"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

type privateAfterCommitError struct {
	mode      string
	formatted int
}

func (e *privateAfterCommitError) Error() string {
	e.formatted++
	switch e.mode {
	case "panic":
		panic("private-hook-value")
	case "goexit":
		runtime.Goexit()
	}
	return "private-hook-value"
}
func TestORMAfterCommitNeverFormatsLocalOrObserverErrors(t *testing.T) {
	for _, source := range []string{"local", "observer"} {
		for _, mode := range []string{"return", "panic", "goexit"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				cause := &privateAfterCommitError{mode: mode}
				calls := 0
				fail := func(context.Context, lifecycle.Operation, orm.Changes[hookqueries.Member]) error {
					calls++
					return cause
				}
				trace := &hookqueries.Trace{}
				var declarations []lifecycle.Declaration
				if source == "local" {
					trace.ORMAfterCommit = fail
				} else {
					declaration, err := orm.NewObserver[hookqueries.Member]("private-error").Declare(func() orm.Hooks[hookqueries.Member] { return orm.Hooks[hookqueries.Member]{AfterCommit: fail} })
					if err != nil {
						t.Fatal(err)
					}
					declarations = append(declarations, declaration)
				}
				observers, err := lifecycle.NewObservers(declarations...)
				if err != nil {
					t.Fatal(err)
				}
				id, err := model.NewIDAt[hookqueries.Member](time.Now())
				if err != nil {
					t.Fatal(err)
				}
				db, err := database.Prepare(database.Adapter{Connector: afterCommitConnector{id.String()}}, database.DefaultPoolConfig())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(context.Background()); err != nil {
						t.Error(err)
					}
				})
				if err = db.BindObservers(observers); err != nil {
					t.Fatal(err)
				}
				if err = db.Start(t.Context()); err != nil {
					t.Fatal(err)
				}
				_, err = hookqueries.Members.Insert(hookqueries.WithTrace(t.Context(), trace), db, hookqueries.Members.Key().Set(id), hookqueries.Members.Email.Set("stored@example.test"), hookqueries.Members.IntroducerID.Set(id))
				var original *privateAfterCommitError
				var outcome *database.Error
				if !errors.Is(err, cause) || !errors.As(err, &original) || original != cause || !errors.As(err, &outcome) || outcome.Outcome() != database.Committed || !errors.Is(err, database.AfterCommitFailed) {
					t.Fatal("ORM callback lost cause or outcome", err)
				}
				for _, s := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
					if strings.Contains(s, "private-hook-value") {
						t.Fatal("ORM callback disclosed application text")
					}
				}
				if cause.formatted != 0 || calls != 1 || db.Stats().Owners != 0 {
					t.Fatal("ORM callback formatted, retried or leaked ownership", cause.formatted, calls)
				}
			})
		}
	}
}

// Real database.Rows and transactions exercise the ORM adapter without PostgreSQL.
type afterCommitConnector struct{ id string }

func (c afterCommitConnector) Connect(context.Context) (driver.Conn, error) {
	return afterCommitConnection{c.id}, nil
}
func (c afterCommitConnector) Driver() driver.Driver { return c }
func (c afterCommitConnector) Open(string) (driver.Conn, error) {
	return afterCommitConnection{c.id}, nil
}

type afterCommitConnection struct{ id string }

func (c afterCommitConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c afterCommitConnection) Close() error               { return nil }
func (c afterCommitConnection) Ping(context.Context) error { return nil }
func (c afterCommitConnection) Begin() (driver.Tx, error)  { return afterCommitTransaction{}, nil }
func (c afterCommitConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c afterCommitConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (c afterCommitConnection) QueryContext(_ context.Context, sql string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(sql, `"hook_logs"`) {
		return &afterCommitRows{columns: []string{"id", "label"}, values: []driver.Value{c.id, "logged"}}, nil
	}
	return &afterCommitRows{columns: []string{"id", "email", "nickname", "introducer_id", "is_introducer", "rank"}, values: []driver.Value{c.id, "stored@example.test", nil, c.id, false, int64(1)}}, nil
}

type afterCommitTransaction struct{}

func (afterCommitTransaction) Commit() error   { return nil }
func (afterCommitTransaction) Rollback() error { return nil }

type afterCommitRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (r *afterCommitRows) Columns() []string { return r.columns }
func (r *afterCommitRows) Close() error      { return nil }
func (r *afterCommitRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}
