package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"database/sql/driver"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

func TestCursorChunksAndLockedLookupRouting(t *testing.T) {
	r := records()
	var primary, replica []string
	columns := []string{"id", "display_name", "note", "amount"}
	values := [][]driver.Value{{int64(1), "name", nil, "1"}}
	db := testDB(t, testConnector{statements: &primary, columns: columns, values: values}, database.WithReadPool(func() (database.Adapter, error) {
		return database.Adapter{Connector: testConnector{statements: &replica, columns: columns, values: values}}, nil
	}, database.DefaultPoolConfig(), 2*database.DefaultPoolConfig().MaxOpen))
	if _, err := orm.CursorPaginate(t.Context(), db, r.Query(), orm.CursorRequest[record]{Size: 2}); err != nil {
		t.Fatal(err)
	}
	if err := orm.ChunkByKey(t.Context(), db, r.Query().Limit(1), orm.KeyExpr(r.ID), 2, func([]record) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(replica) != 2 || len(primary) != 0 {
		t.Fatal("unlocked reads did not route to read pool", primary, replica)
	}
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := orm.RequireLocked(t.Context(), tx, orm.Lock(r.Query().Where(orm.KeyEq(r.ID, 1))))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(primary) != 1 || len(replica) != 2 {
		t.Fatal("locked lookup left transaction", primary, replica)
	}
	if _, err := orm.Raw(t.Context(), db, r.Table, "SELECT id,display_name,note,amount FROM records"); err != nil {
		t.Fatal(err)
	}
	if len(primary) != 2 || len(replica) != 2 {
		t.Fatal("raw unexpectedly used read routing", primary, replica)
	}

}
