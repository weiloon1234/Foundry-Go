package rows

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
	records := ConflictRowsOf(m.Query())
	policy := OnConflictKeys(expr.Group(expr.Value(m.Name))).DoUpdate(SetConflictValue(m.Nickname, expr.At(records.Proposed(), m.Nickname)))
	policy = ConflictWhereRows(policy, expr.At(records.Stored(), m.Name).Ne("blocked"))
	for i := 0; i < 3; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "stored", nil}}}
			db, err := database.Open(t.Context(), database.Adapter{Connector: conflictDriver{testDriver{state}}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close(context.Background()) })
			_ = ConflictWhereRows(policy, expr.At(records.Proposed(), m.Name).Eq("unused"))
			got, err := orm.Upsert(t.Context(), db, m.Query(), policy, m.ID.Set(1), m.Name.Set("input"))
			if err != nil || !got.IsSet() || !state.closed || strings.Contains(state.sql, "unused") || !strings.Contains(state.sql, `"nickname" = "excluded"."nickname"`) {
				t.Fatal(err, got, state.sql, state.closed)
			}
		})
	}
}

func TestConflictRejectsOtherDeclarationsBeforeTransaction(t *testing.T) {
	m, other := declareMembers(), declareMembers()
	records := ConflictRowsOf(other.Query())
	state := &driverState{}
	policy := orm.OnConflict(orm.ConflictKey(m.ID)).DoUpdate(SetConflictValue(m.Name, expr.At(records.Proposed(), other.Name)))
	_, err := orm.Upsert(t.Context(), openTest(t, state), m.Query(), policy, m.ID.Set(1), m.Name.Set("input"))
	if !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}
