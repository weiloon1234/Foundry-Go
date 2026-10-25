package tooling_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"strings"
	"testing"

	"foundry.test/consumer/models"
	"foundry.test/consumer/tooling"
	"github.com/weiloon1234/Foundry-Go/database"
)

// The driver observes the real database execution boundary without a server.
type recordedQuery struct {
	statement string
	arguments []driver.NamedValue
	cause     error
}
type recordingConnector struct{ query *recordedQuery }

func (c recordingConnector) Driver() driver.Driver { return c }
func (c recordingConnector) Open(string) (driver.Conn, error) {
	return recordingConnection{c.query}, nil
}
func (c recordingConnector) Connect(context.Context) (driver.Conn, error) { return c.Open("") }

type recordingConnection struct{ query *recordedQuery }

func (recordingConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (recordingConnection) Close() error              { return nil }
func (recordingConnection) Begin() (driver.Tx, error) { return recordingTransaction{}, nil }
func (c recordingConnection) QueryContext(_ context.Context, statement string, arguments []driver.NamedValue) (driver.Rows, error) {
	c.query.statement = statement
	c.query.arguments = append([]driver.NamedValue(nil), arguments...)
	return nil, c.query.cause
}

type recordingTransaction struct{}

func (recordingTransaction) Commit() error   { return nil }
func (recordingTransaction) Rollback() error { return nil }
func recordingDatabase(t *testing.T) (*database.DB, *recordedQuery) {
	t.Helper()
	recorded := &recordedQuery{cause: errors.New("stop before hydration")}
	db, err := database.Open(t.Context(), database.Adapter{Connector: recordingConnector{recorded}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db, recorded
}

func TestFactoryRecipeBindsSequencedNames(t *testing.T) {
	records, err := tooling.Records()
	if err != nil {
		t.Fatal(err)
	}
	db, recorded := recordingDatabase(t)
	for _, name := range []string{"record-1", "record-2"} {
		if _, err := records.Create(t.Context(), db); !errors.Is(err, recorded.cause) {
			t.Fatal("factory failure identity changed", err)
		}
		if !strings.HasPrefix(recorded.statement, `INSERT INTO "write_records"`) || len(recorded.arguments) != 1 || recorded.arguments[0].Value != name {
			t.Fatalf("factory recipe lost sequenced name: %s %#v", recorded.statement, recorded.arguments)
		}
	}
}

func TestUserPlanAndRoutingSQLParity(t *testing.T) {
	db, recorded := recordingDatabase(t)
	for _, pair := range []struct {
		name         string
		old, current func() error
	}{
		{"explain", func() error { _, err := models.QueryUsers().Limit(10).Explain(t.Context(), db); return err }, func() error { _, err := tooling.UserPlan(t.Context(), db); return err }},
		{"read", func() error { _, err := models.QueryUsers().Limit(10).All(t.Context(), db); return err }, func() error { _, err := tooling.ReadUsers(t.Context(), db); return err }},
		{"primary", func() error { _, err := models.QueryUsers().Limit(10).All(t.Context(), db.Primary()); return err }, func() error { _, err := tooling.ReadPrimaryUsers(t.Context(), db); return err }},
	} {
		t.Run(pair.name, func(t *testing.T) {
			oldErr := pair.old()
			if !errors.Is(oldErr, recorded.cause) {
				t.Fatal(oldErr)
			}
			sql, args := recorded.statement, recorded.arguments
			currentErr := pair.current()
			if !errors.Is(currentErr, recorded.cause) {
				t.Fatal(currentErr)
			}
			if sql != recorded.statement || !reflect.DeepEqual(args, recorded.arguments) || !oracleEqual(t, "consumer/tooling/database_test/1", oracle.Snapshot([]any{sql, args, errors.Is(oldErr, recorded.cause)}), oracle.Snapshot([]any{recorded.statement, recorded.arguments, errors.Is(currentErr, recorded.cause)})) {
				t.Fatal("old/new SQL or bindings differ", sql, recorded.statement)
			}
		})
	}
	if _, err := tooling.LockedUserPlan(t.Context(), nil); err == nil {
		t.Fatal("locked plan accepted absent transaction")
	}
}
