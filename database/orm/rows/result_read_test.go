package rows

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
)

func TestResultReadTerminals(t *testing.T) {
	m := declareMembers()
	q := Read(SelectValue(m.Query(), m.Name))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{"Ada"}, {"Lin"}}}
	db := openTest(t, state)
	page, err := q.OrderBy(expr.Value(m.ID).Asc()).SimplePaginate(t.Context(), db, orm.PageRequest{Number: 1, Size: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0] != "Ada" || !page.HasMore || !state.closed {
		t.Fatal(page, err)
	}
	state.rows = nil
	if first, err := q.First(t.Context(), db); err != nil || first.IsSet() {
		t.Fatal(first, err)
	}
	if _, err := q.RequireFirst(t.Context(), db); !errors.Is(err, database.NotFound) {
		t.Fatal(err)
	}
	state.rows = [][]driver.Value{{int64(7)}}
	if count, err := q.Limit(2).Offset(1).Count(t.Context(), db); err != nil || count != 7 || !strings.Contains(state.sql, `) AS "foundry_count"`) {
		t.Fatal(count, err, state.sql)
	}
	state.rows = [][]driver.Value{{true}}
	if exists, err := q.Exists(t.Context(), db); err != nil || !exists || !strings.HasPrefix(state.sql, "SELECT EXISTS(") {
		t.Fatal(exists, err, state.sql)
	}
	state.sql = ""
	for _, bad := range []Result[member, string]{q, q.OrderBy(expr.Value(m.ID).Asc()).Limit(1), q.OrderBy(expr.Value(m.ID).Asc()).Offset(1)} {
		if _, err := bad.Paginate(t.Context(), db, orm.PageRequest{Number: 1, Size: 1}); !errors.Is(err, fault.Invalid) || state.sql != "" {
			t.Fatal(err, state.sql)
		}
	}
}

func TestResultLocksUseSharedCompilerAndPreservedTargets(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	a, b := As[left](m.Query(), "m"), As[right](o.Query(), "o")
	j := LeftJoin(a, b, On(expr.At(Scope(a), m.ID), expr.At(Scope(b), o.MemberID)))
	read := SelectRecord(j, LeftScope(j, Scope(a)))
	target := Target(LeftScope(j, Scope(a)))
	q := ForUpdate(read).Of(target).SkipLocked()
	statement, err := ormcore.CompileRecord(t.Context(), q.core)
	if err != nil || !strings.Contains(statement.SQLText, `FOR UPDATE OF "m" SKIP LOCKED`) {
		t.Fatal(statement.SQLText, err)
	}
	if len(read.core.Source.Node.Locks) != 0 {
		t.Fatal("input mutated")
	}
	for _, bad := range []LockedResult[Left[Alias[left, member], Alias[right, order]], member]{ForUpdate(read), q.Of(), q.Of(target, target), LockRows(read, RowLock[Left[Alias[left, member], Alias[right, order]]]{}), q.Of(Target(LeftScope(j, Scope(As[left](declareMembers().Query(), "m")))))} {
		if _, err := ormcore.CompileRecord(t.Context(), bad.core); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	multi := LockRows(Read(FromModel(m.Query())), UpdateLock(Target(ModelScope(m.Query()))).NoWait())
	if _, err := ormcore.CompileRecord(t.Context(), multi.core); err != nil {
		t.Fatal(err)
	}
	txsource := AsTransaction[left](TransactionOf(ModelRecord(m.Query())), "txm")
	scalar := SelectTransactionValue(txsource, expr.Row(expr.At(TransactionScope(txsource), m.ID)).Value())
	locked := ForKeyShare(scalar).Of(Target(TransactionScope(txsource)))
	if s, err := ormcore.CompileRecord(t.Context(), locked.core); err != nil || !strings.Contains(s.SQLText, "FOR KEY SHARE") {
		t.Fatal(s.SQLText, err)
	}
	state := &driverState{}
	if _, err := scalar.All(t.Context(), nil); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err)
	}
	if _, err := q.All(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
