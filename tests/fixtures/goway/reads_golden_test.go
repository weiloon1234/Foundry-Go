package goway_test

import (
	"context"
	"database/sql/driver"
	"testing"

	"foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Keep the historical concrete model identity while using only handwritten ORM.
type cursorUser = oldmodels.User

func readUserTable() oldmodels.UserTable { return oldmodels.Users }

var readUserColumns = []string{"id", "email_address", "age", "nickname", "status", "level", "birthday", "introducer_id"}

func readUser(id string, age int64, nick driver.Value) []driver.Value {
	return []driver.Value{id, "a@example.test", age, nick, "active", int64(1), nil, nil}
}
func readDB(t *testing.T, g *goldenCapture) *database.DB {
	t.Helper()
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{g}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}
func TestReadSQLAndCursorTokensAgree(t *testing.T) {
	u := readUserTable()
	if err := orm.Check(u); err != nil {
		t.Fatal(err)
	}
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary", true: "nullable_mixed"}[mixed], func(t *testing.T) {
			q := u.Query()
			if mixed {
				q = q.OrderBy(orm.NullableTextAsc(u.Nickname), orm.NumberDesc(u.Age))
			}
			a, b := readUser(orderID, 30, nil), readUser(buyerID, 20, "Ada")
			capture := &goldenCapture{scriptedColumns: readUserColumns}
			db := readDB(t, capture)
			var page orm.CursorPage[cursorUser]
			for step := 0; step < 3; step++ {
				request := orm.CursorRequest[cursorUser]{Size: 1}
				batch := [][]driver.Value{a, b}
				if step == 1 {
					request.After = page.Next
					batch = [][]driver.Value{b}
				}
				if step == 2 {
					request.Before = page.Previous
					batch = [][]driver.Value{a}
				}
				capture.scriptedBatches = [][][]driver.Value{batch}
				var err error
				page, err = orm.CursorPaginate(t.Context(), db, q, request)
				if err != nil {
					t.Fatal(err)
				}
				if !oracleEqual(t, "reads_golden/pages", oracleSnapshot(page)) {
					t.Fatal("frozen cursor results or token bytes differ")
				}
			}
			if !oracleEqual(t, "reads_golden/1", capture.output.String()) {
				t.Fatal("frozen cursor SQL differs")
			}
		})
	}
	capture := &goldenCapture{scriptedColumns: readUserColumns}
	db := readDB(t, capture)
	id, err := model.ParseID[cursorUser](orderID)
	if err != nil {
		t.Fatal(err)
	}
	capture.scriptedBatches = [][][]driver.Value{{readUser(orderID, 1, nil)}, {readUser(buyerID, 2, nil)}, {}}
	seen := 0
	visit := func(items []cursorUser) error { seen += len(items); capture.show(items); return nil }
	err = orm.ChunkByKey(t.Context(), db, u.Query(), orm.KeyExpr(u.Key()), 1, visit)
	if err != nil || seen != 2 {
		t.Fatal("key chunks", err, seen)
	}
	capture.scriptedBatches = [][][]driver.Value{{readUser(orderID, 1, nil)}}
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := orm.RequireLocked(t.Context(), tx, orm.Lock(u.Query().Where(orm.KeyEq(u.Key(), id))))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !oracleEqual(t, "reads_golden/2", capture.output.String()) {
		t.Fatal("frozen key read SQL differs")
	}
}
