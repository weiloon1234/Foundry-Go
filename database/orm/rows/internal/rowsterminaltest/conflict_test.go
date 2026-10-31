package rowsterminaltest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type conflictDriver struct{ testDriver }

func (d conflictDriver) Connect(context.Context) (driver.Conn, error) {
	return &conflictConn{testConn{d.state}}, nil
}

type conflictConn struct{ testConn }

func (*conflictConn) Begin() (driver.Tx, error) { return conflictTx{}, nil }

type conflictTx struct{}

func (conflictTx) Commit() error   { return nil }
func (conflictTx) Rollback() error { return nil }

func TestConflictPolicyConcurrentReuseAndRowClosure(t *testing.T) {
	m := declareMembers()
	records := rows.ConflictRowsOf(m.Query())
	policy := rows.OnConflictKeys(expr.Group(expr.Value(orm.TextExpr(m.Name)))).DoUpdate(rows.SetConflictValue(orm.NullableTextExpr(m.Nickname), expr.At(records.Proposed(), orm.NullableTextExpr(m.Nickname))))
	policy = rows.ConflictWhereRows(policy, expr.Ne(expr.At(records.Stored(), orm.TextExpr(m.Name)), "blocked"))
	for i := 0; i < 3; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "stored", nil}}}
			db, err := database.Open(t.Context(), database.Adapter{Connector: conflictDriver{testDriver{state}}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close(context.Background()) })
			_ = rows.ConflictWhereRows(policy, expr.Eq(expr.At(records.Proposed(), orm.TextExpr(m.Name)), "unused"))
			got, err := orm.Upsert(t.Context(), db, m.Query(), policy, orm.KeySet(m.ID, 1), orm.TextSet(m.Name, "input"))
			if err != nil || !got.IsSet() || !state.closed || strings.Contains(state.sql, "unused") || !strings.Contains(state.sql, `"nickname" = "excluded"."nickname"`) {
				t.Fatal(err, got, state.sql, state.closed)
			}
		})
	}
}

func TestConflictRejectsOtherDeclarationsBeforeTransaction(t *testing.T) {
	m, other := declareMembers(), declareMembers()
	records := rows.ConflictRowsOf(other.Query())
	state := &driverState{}
	policy := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(m.ID))).DoUpdate(rows.SetConflictValue(orm.TextExpr(m.Name), expr.At(records.Proposed(), orm.TextExpr(other.Name))))
	_, err := orm.Upsert(t.Context(), openTest(t, state), m.Query(), policy, orm.KeySet(m.ID, 1), orm.TextSet(m.Name, "input"))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}
func TestSharedWritePlanningOwnsCapturedPoliciesDuringConcurrentCompilation(t *testing.T) {
	m := declareMembers()
	updates := []orm.ConflictUpdate[member]{orm.ConflictSet(orm.NullableTextExpr(m.Nickname), "captured")}
	policy := rows.ConflictTargetWhere(rows.OnConflictKeys(expr.Group(expr.Lower(expr.Value(orm.TextExpr(m.Name))))), expr.Ne(expr.Value(orm.TextExpr(m.Name)), "")).DoUpdate(updates...)
	updates[0] = orm.ConflictNull(orm.NullableTextExpr(m.Nickname))
	for i := range 12 {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "inserted", "captured"}}}
			db, e := database.Open(t.Context(), database.Adapter{Connector: conflictDriver{testDriver{state}}}, database.DefaultPoolConfig())
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { db.Close(context.Background()) })
			_ = rows.ConflictTargetWhere(policy.Where(orm.TextEq(m.Name, "allowed")), expr.Ne(expr.Value(orm.TextExpr(m.Name)), "other")).DoUpdate(orm.ConflictIncoming(orm.NullableTextExpr(m.Nickname)))
			for range 2 {
				if _, e := orm.Upsert(t.Context(), db, m.Query(), policy, orm.KeySet(m.ID, 1), orm.TextSet(m.Name, "inserted")); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(state.sql, `WHERE ("name" <> E'')`) || strings.Contains(state.sql, "allowed") || len(state.arguments) != 3 || state.arguments[0].Value != int64(1) || state.arguments[1].Value != "inserted" || state.arguments[2].Value != "captured" {
					t.Fatal("borrowed captured policy", state.sql, state.arguments)
				}
			}
		})
	}
}
