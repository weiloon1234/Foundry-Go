package goway_test

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"

	"foundry.test/goway/oldmodels"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
)

// The same Go model proves the token's original model identity is preserved.
type readUsers struct {
	orm.Table[oldmodels.User, model.ID[oldmodels.User]]
	Age      orm.Number[oldmodels.User, int]
	Nickname orm.NullableText[oldmodels.User, string]
}

func readUserTable() readUsers {
	return orm.Define("users", func(b orm.Builder[oldmodels.User], u *oldmodels.User) readUsers {
		orm.TextOf(b, &u.Email)
		orm.EnumOf(b, &u.Status)
		orm.EnumOf(b, &u.Level)
		orm.NullableTimeOf(b, &u.Birthday)
		orm.NullableIDOf(b, &u.IntroducerID)
		return readUsers{orm.TableOf(b, &u.ID), orm.NumberOf(b, &u.Age), orm.NullableTextOf(b, &u.Nickname)}
	})
}

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
	f := oldmodels.UserFields()
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary", true: "nullable_mixed"}[mixed], func(t *testing.T) {
			oldQ := oldmodels.QueryUsers().Query
			newQ := u.Query()
			if mixed {
				oldQ = oldQ.OrderBy(f.Nickname.Asc(), f.Age.Desc())
				newQ = newQ.OrderBy(u.Nickname.Asc(), u.Age.Desc())
			}
			a, b := readUser(orderID, 30, nil), readUser(buyerID, 20, "Ada")
			captures := []*goldenCapture{{scriptedColumns: readUserColumns}, {scriptedColumns: readUserColumns}}
			dbs := []*database.DB{readDB(t, captures[0]), readDB(t, captures[1])}
			pages := make([]query.CursorPage[oldmodels.User], 2)
			for step := 0; step < 3; step++ {
				previous := append([]query.CursorPage[oldmodels.User](nil), pages...)
				for i := range 2 {
					request := query.CursorRequest[oldmodels.User]{Size: 1}
					batch := [][]driver.Value{a, b}
					if step == 1 {
						request.After = previous[1-i].Next
						batch = [][]driver.Value{b}
					}
					if step == 2 {
						request.Before = previous[1-i].Previous
						batch = [][]driver.Value{a}
					}
					captures[i].scriptedBatches = [][][]driver.Value{batch}
					var err error
					if i == 0 {
						pages[i], err = oldQ.CursorPaginate(t.Context(), dbs[i], request)
					} else {
						pages[i], err = orm.CursorPaginate(t.Context(), dbs[i], newQ, request)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(pages[0], pages[1]) || !oracleEqual(t, "reads_golden/pages", oracleSnapshot(pages[0]), oracleSnapshot(pages[1])) {
					t.Fatal("old/new cursor results or token bytes differ")
				}
			}
			if !oracleEqual(t, "reads_golden/1", captures[0].output.String(), captures[1].output.String()) {
				t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
			}
		})
	}
	// Primary-key chunks and locked keyed lookups compile to the same SQL/bindings.
	captures := []*goldenCapture{{scriptedColumns: readUserColumns}, {scriptedColumns: readUserColumns}}
	id, err := model.ParseID[oldmodels.User](orderID)
	if err != nil {
		t.Fatal(err)
	}
	for i, g := range captures {
		db := readDB(t, g)
		g.scriptedBatches = [][][]driver.Value{{readUser(orderID, 1, nil)}, {readUser(buyerID, 2, nil)}, {}}
		seen := 0
		visit := func(items []oldmodels.User) error { seen += len(items); g.show(items); return nil }
		if i == 0 {
			err = oldmodels.QueryUsers().ChunkByID(t.Context(), db, 1, visit)
		} else {
			err = orm.ChunkByKey(t.Context(), db, u.Query(), u.Key(), 1, visit)
		}
		if err != nil || seen != 2 {
			t.Fatal("key chunks", err, seen)
		}
		g.scriptedBatches = [][][]driver.Value{{readUser(orderID, 1, nil)}}
		err = db.Transaction(t.Context(), func(tx *database.Tx) error {
			if i == 0 {
				_, e := oldmodels.QueryUsers().ForUpdate().RequireFind(t.Context(), tx, id)
				return e
			}
			_, e := u.Query().Where(u.Key().Eq(id)).ForUpdate().Require(t.Context(), tx)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !oracleEqual(t, "reads_golden/2", captures[0].output.String(), captures[1].output.String()) {
		t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
	}
}
