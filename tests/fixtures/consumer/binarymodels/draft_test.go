package binarymodels

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

// The real transaction and hook pipeline must stop at the inspecting callback.
// Any attempt to send a statement instead fails the test through this connector.
func draftPool(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Prepare(database.Adapter{Connector: draftConnector{}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := db.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

type draftConnector struct{}

func (c draftConnector) Connect(context.Context) (driver.Conn, error) { return draftConnection{}, nil }
func (c draftConnector) Driver() driver.Driver                        { return c }
func (c draftConnector) Open(string) (driver.Conn, error)             { return draftConnection{}, nil }

type draftConnection struct{}

func (draftConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected SQL") }
func (draftConnection) Close() error                        { return nil }
func (draftConnection) Ping(context.Context) error          { return nil }
func (draftConnection) Begin() (driver.Tx, error)           { return draftTransaction{}, nil }
func (c draftConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

type draftTransaction struct{}

func (draftTransaction) Commit() error   { return errors.New("inspected draft unexpectedly committed") }
func (draftTransaction) Rollback() error { return nil }
