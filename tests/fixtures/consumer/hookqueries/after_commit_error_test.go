package hookqueries_test

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

type privateAfterCommitFormatter struct {
	*privateAfterCommitError
	formatCalls int
}

func (e *privateAfterCommitFormatter) Format(state fmt.State, _ rune) {
	e.formatCalls++
	switch e.mode {
	case "format-panic":
		panic("private-hook-value")
	case "format-goexit":
		runtime.Goexit()
	default:
		fmt.Fprint(state, "private-hook-value")
	}
}

func TestGeneratedAfterCommitNeverFormatsLocalOrObserverErrors(t *testing.T) {
	for _, source := range []string{"local", "observer"} {
		for _, mode := range []string{"return", "panic", "goexit", "format-return", "format-panic", "format-goexit"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				application := &privateAfterCommitError{mode: mode}
				formatter := &privateAfterCommitFormatter{privateAfterCommitError: application}
				var cause error = application
				if strings.HasPrefix(mode, "format-") {
					cause = formatter
				}
				calls := 0
				fail := func(context.Context, lifecycle.Operation, hookqueries.MemberChanges) error { calls++; return cause }
				trace := &hookqueries.Trace{}
				var declarations []lifecycle.Declaration
				if source == "local" {
					trace.AfterCommit = fail
				} else {
					declaration, err := hookqueries.NewMemberObserver("private-error").Declare(func() hookqueries.MemberHooks { return hookqueries.MemberHooks{AfterCommit: fail} })
					if err != nil {
						t.Fatal(err)
					}
					declarations = append(declarations, declaration)
				}
				observers, err := lifecycle.NewObservers(declarations...)
				if err != nil {
					t.Fatal(err)
				}
				id, err := model.NewID[hookqueries.Member]()
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
				_, err = hookqueries.QueryHookMembers().Create(hookqueries.WithTrace(t.Context(), trace), db, hookqueries.MemberDraft{}.SetID(id).SetEmail("stored@example.test").SetIntroducerID(id))
				var original *privateAfterCommitError
				var outcome *database.Error
				identity := errors.As(err, &original) && original == application
				if cause == formatter {
					var originalFormatter *privateAfterCommitFormatter
					identity = errors.As(err, &originalFormatter) && originalFormatter == formatter
				}
				if !errors.Is(err, cause) || !identity || !errors.As(err, &outcome) || outcome.Outcome() != database.Committed || !errors.Is(err, database.AfterCommitFailed) {
					t.Fatal("generated callback lost cause or outcome", err)
				}
				for _, s := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
					if strings.Contains(s, "private-hook-value") {
						t.Fatal("generated callback disclosed application text")
					}
				}
				if application.formatted != 0 || formatter.formatCalls != 0 || calls != 1 || db.Stats().Owners != 0 {
					t.Fatal("generated callback formatted, retried or leaked ownership", application.formatted, formatter.formatCalls, calls)
				}
			})
		}
	}
}

// Real database.Rows and transactions exercise the generated adapter without PostgreSQL.
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
