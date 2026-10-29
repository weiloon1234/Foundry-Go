package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

func TestSourceWritePlanConcurrentReuse(t *testing.T) {
	m := declareMembers()
	p := InsertFrom(From(m.Query().Limit(2)), m.Query(), MapInsert(orm.KeyExpr(m.ID), expr.Value(orm.KeyExpr(m.ID))), MapInsert(orm.TextExpr(m.Name), expr.Value(orm.TextExpr(m.Name))))
	for _, name := range []string{"a", "b", "c"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "stored", nil}}}
			db, err := database.Open(t.Context(), database.Adapter{Connector: conflictDriver{testDriver{state}}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close(context.Background()) })
			_ = p.Values(orm.NullableTextSet(m.Nickname, "unused"))
			got, err := p.Returning(t.Context(), db, 2)
			if err != nil || len(got) != 1 || !state.closed || strings.Contains(strings.Split(state.sql, " RETURNING")[0], "nickname") {
				t.Fatal(got, err, state.closed, state.sql)
			}
		})
	}
}
