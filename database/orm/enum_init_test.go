package orm_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/internal/enuminit"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestEnumsInitializeAfterModelDeclaration(t *testing.T) {
	// A frozen concrete owner proves observer absence. Unknown transactor wrappers
	// must now enter their transaction to discover observers that can repair input.
	db, err := database.Open(t.Context(), database.Adapter{Connector: enumRefusingConnector{}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})

	for index := range 3 {
		as := []orm.Assignment[enuminit.Model]{enuminit.Models.Key().Set(enuminit.Active), enuminit.Models.State.Set(enuminit.Active), enuminit.Models.Nullable.Set(enuminit.Active)}
		bad := enuminit.Status("unknown")
		switch index {
		case 0:
			as[0] = enuminit.Models.Key().Set(bad)
		case 1:
			as[1] = enuminit.Models.State.Set(bad)
		case 2:
			as[2] = enuminit.Models.Nullable.Set(bad)
		}
		if _, err := enuminit.Models.Insert(t.Context(), db, as...); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if err := orm.Check(enuminit.Models); err != nil {
		t.Fatal(err)
	}
	if err := rows.Check(enuminit.Rows); err != nil {
		t.Fatal(err)
	}

}

type enumRefusingConnector struct{}

func (enumRefusingConnector) Connect(context.Context) (driver.Conn, error) {
	return enumRefusingConnection{}, nil
}
func (enumRefusingConnector) Driver() driver.Driver            { return enumRefusingConnector{} }
func (enumRefusingConnector) Open(string) (driver.Conn, error) { return enumRefusingConnection{}, nil }

type enumRefusingConnection struct{}

func (enumRefusingConnection) Prepare(string) (driver.Stmt, error) {
	panic("invalid enum prepared SQL")
}
func (enumRefusingConnection) Close() error               { return nil }
func (enumRefusingConnection) Begin() (driver.Tx, error)  { panic("invalid enum opened a transaction") }
func (enumRefusingConnection) Ping(context.Context) error { return nil }
func (enumRefusingConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	panic("invalid enum queried SQL")
}
