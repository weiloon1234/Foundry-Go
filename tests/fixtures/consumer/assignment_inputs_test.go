package consumer_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Inspect the real operation-local draft before persistence. The driver supports
// only transaction ownership: a missed callback cannot succeed by returning a
// model that happens to contain the expected values.
func inspectInsertAssignments[M any, K comparable](t *testing.T, table orm.Table[M, K], assignments []orm.Assignment[M], inspect func(*orm.Write[M])) {
	t.Helper()
	stop := errors.New("assignment inspection complete")
	calls := 0
	observer, err := orm.NewObserver[M]("consumer.assignment-inputs").Declare(func() orm.Hooks[M] {
		return orm.Hooks[M]{Creating: func(_ context.Context, _ *database.Tx, write *orm.Write[M]) error {
			calls++
			inspect(write)
			return stop
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	observers, err := lifecycle.NewObservers(observer)
	if err != nil {
		t.Fatal(err)
	}
	connection := &assignmentConnection{}
	db, err := database.Prepare(database.Adapter{Connector: assignmentConnector{connection}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := db.BindObservers(observers); err != nil {
		t.Fatal(err)
	}
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = table.Insert(t.Context(), db, assignments...)
	if !errors.Is(err, stop) || calls != 1 || connection.begins != 1 || connection.rollbacks != 1 {
		t.Fatalf("assignment inspection did not consume the draft and roll back: calls=%d begin=%d rollback=%d err=%v", calls, connection.begins, connection.rollbacks, err)
	}
}

type assignmentConnector struct{ connection *assignmentConnection }

func (c assignmentConnector) Driver() driver.Driver                        { return c }
func (c assignmentConnector) Open(string) (driver.Conn, error)             { return c.connection, nil }
func (c assignmentConnector) Connect(context.Context) (driver.Conn, error) { return c.connection, nil }

type assignmentConnection struct{ begins, rollbacks int }

func (*assignmentConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("assignment inspection must stop before SQL")
}
func (*assignmentConnection) Close() error               { return nil }
func (*assignmentConnection) Ping(context.Context) error { return nil }
func (c *assignmentConnection) Begin() (driver.Tx, error) {
	c.begins++
	return c, nil
}
func (*assignmentConnection) Commit() error {
	return errors.New("assignment inspection must not commit")
}
func (c *assignmentConnection) Rollback() error { c.rollbacks++; return nil }
