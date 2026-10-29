package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestCompleteRecordAliasesAndWindows(t *testing.T) {
	m := declareMembers()
	schema := declareRows()
	q := Limit(Offset(OrderBy(Select(From(m.Query().Where(orm.KeyNe(m.ID, 0))), schema, Map(schema.Name, expr.Value(orm.TextExpr(m.Name))), Map(schema.ID, expr.Value(orm.KeyExpr(m.ID)))), expr.Value(orm.KeyExpr(m.ID)).Desc()), 2), 3)
	a := AsRecord[left](RecordOf(q), "report")
	selected := SelectRecord(a, Scope(a)).Where(At(Scope(a), schema.ID).Ne(0)).Limit(1)
	state := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(3), "Ada"}}}
	got, err := ReadRecord(selected).All(t.Context(), openTest(t, state))
	if err != nil || !reflect.DeepEqual(got, []record{{3, "Ada"}}) {
		t.Fatal(got, err)
	}
	for _, part := range []string{`FROM (SELECT`, `ORDER BY "members"."id" DESC LIMIT $2 OFFSET $3) AS "report"`, `"report"."id"`, `LIMIT $5`} {
		if !strings.Contains(state.sql, part) {
			t.Fatal(state.sql)
		}
	}
	if selected.core.Record.Owner != schema.Row.core {
		t.Fatal("declaration changed")
	}
	_ = selected.Offset(10)
	if selected.core.Source.Node.Offset != 0 {
		t.Fatal("window mutated")
	}
}

func TestSelectRecordPreservedModelAndForeignDeclaration(t *testing.T) {
	m, o := declareMembers(), declareOrders()
	a, b := As[left](m.Query(), "a"), As[right](o.Query(), "b")
	j := LeftJoin(a, b, On(expr.At(Scope(a), orm.KeyExpr(m.ID)), expr.At(Scope(b), orm.NumberExpr(o.MemberID))))
	good := SelectRecord(j, LeftScope(j, Scope(a)))
	state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "Ada", nil}}}
	if got, err := ReadRecord(good).All(t.Context(), openTest(t, state)); err != nil || len(got) != 1 || got[0].Name != "Ada" {
		t.Fatal(got, err)
	}
	foreign := As[left](declareMembers().Query(), "a")
	bad := SelectRecord(j, LeftScope(j, Scope(foreign)))
	state = &driverState{}
	if got, err := ReadRecord(bad).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal(got, err, state.sql)
	}
	// Even a forged preserved scope must not bypass the shared nullable-side check.
	data := expr.FoundryNullableScopeData(NullableRightScope(j, Scope(b)), sqlowner.Expressions())
	forged := expr.FoundryScope[Left[Alias[left, member], Alias[right, order]], order](data, sqlowner.Expressions())
	state = &driverState{}
	if _, err := ReadRecord(SelectRecord(j, forged)).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
		t.Fatal(err, state.sql)
	}
}

func TestRecordRejectsIncompleteScalarAndWrongRuntimeType(t *testing.T) {
	m := declareMembers()
	records := []Record[int64]{RecordOf(SelectValue(m.Query(), orm.KeyExpr(m.ID))), {}}
	for _, r := range records {
		a := AsRecord[left](r, "bad")
		state := &driverState{}
		if _, err := ReadRecord(SelectRecord(a, Scope(a))).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
			t.Fatal(err, state.sql)
		}
	}
	r := ModelRecord(m.Query())
	r.core.Record.Type = reflect.TypeFor[order]()
	a := AsRecord[left](r, "bad")
	if !errors.Is(a.source.Err, fault.Invalid) {
		t.Fatal("wrong decoder type accepted")
	}
	r = ModelRecord(m.Query())
	r.core.Transaction = true
	if !errors.Is(AsRecord[left](r, "bad").source.Err, fault.Invalid) {
		t.Fatal("transaction kind erased")
	}
}

func TestRecordModelLifecycleAndFailureOwnership(t *testing.T) {
	for _, bad := range []string{"", "scan", "close", "hook"} {
		t.Run(bad, func(t *testing.T) {
			calls, factories := 0, 0
			state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "Ada", nil}}}
			sentinel := errors.New("record failure")
			if bad == "scan" {
				state.rows = append(state.rows, []driver.Value{"invalid", "Bad", nil})
			}
			if bad == "close" {
				state.closeErr = sentinel
			}
			m := orm.Define("members", func(b orm.Builder[member], r *member) members {
				tbl := orm.TableOf(b, &r.ID)
				return members{tbl, orm.PrimaryOf(b, tbl.Key()), orm.TextOf(b, &r.Name), orm.NullableTextOf(b, &r.Nickname)}
			}, orm.WithRetrievalHooks(func() orm.RetrievalHooks[member] {
				factories++
				return orm.RetrievalHooks[member]{Retrieved: func(_ context.Context, _ database.Executor, r member) error {
					calls++
					if !state.closed || r.Name != "Ada" {
						t.Error("incomplete callback", r)
					}
					if bad == "hook" {
						return sentinel
					}
					return nil
				}}
			}))
			a := AsRecord[left](ModelRecord(m.Query()), "a")
			got, err := ReadRecord(SelectRecord(a, Scope(a))).All(t.Context(), openTest(t, state))
			if bad == "" {
				if err != nil || len(got) != 1 || calls != 1 || factories != 1 {
					t.Fatal(got, err, calls, factories)
				}
			} else {
				if err == nil || got != nil {
					t.Fatal(got, err)
				}
				if bad != "hook" && (calls != 0 || factories != 0) {
					t.Fatal("partial callback", calls, factories)
				}
			}
		})
	}
}

func TestRecordBridgesRejectZeroCapabilities(t *testing.T) {
	m := declareMembers()
	scope := expr.FoundryScopeData(ModelScope(m.Query()), sqlowner.Expressions())
	var zero sqlowner.MetadataSeal
	if r := orm.FoundryRecord[member](scope, zero); !errors.Is(r.Err, fault.Invalid) || r.Scan != nil || r.Owner != nil {
		t.Fatal("record capability exposed", r.Err)
	}
	if r := orm.FoundryLockedRecord(orm.Lock(m.Query()), zero); !errors.Is(r.Source.Err, fault.Invalid) || r.Record.Scan != nil {
		t.Fatal("locked capability exposed", r.Source.Err)
	}
	if r := orm.FoundryRecord[order](scope, sqlowner.Metadata()); !errors.Is(r.Err, fault.Invalid) {
		t.Fatal("model type erased")
	}
}

type recordConnector struct{ state *driverState }

func (c recordConnector) Driver() driver.Driver { return c }
func (c recordConnector) Open(string) (driver.Conn, error) {
	return &recordConn{testConn: testConn{state: c.state}}, nil
}
func (c recordConnector) Connect(context.Context) (driver.Conn, error) { return c.Open("") }

type recordConn struct{ testConn }

func (c *recordConn) Begin() (driver.Tx, error) { return recordTx{}, nil }

type recordTx struct{}

func (recordTx) Commit() error   { return nil }
func (recordTx) Rollback() error { return nil }

func TestTransactionRecordsKeepLocksAndUseCallerTransaction(t *testing.T) {
	m := declareMembers()
	schema := declareRows()
	source := AsTransaction[left](LockedRecord(orm.Lock(m.Query().Where(orm.KeyNe(m.ID, 0))).SkipLocked()), "claim")
	scope := TransactionScope(source)
	selected := SelectTransactionRecord(source, scope)
	statement, err := querycore.CompileRead(selected.core.Source.Node, t.Context(), true, selected.core.Source.Err, true)
	if err != nil || !strings.Contains(statement.SQLText, `FOR UPDATE SKIP LOCKED) AS "claim"`) {
		t.Fatal(statement.SQLText, err)
	}
	if _, err := querycore.CompileRead(selected.core.Source.Node, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) {
		t.Fatal("lock escaped ordinary boundary", err)
	}
	if got, err := selected.All(t.Context(), nil); !errors.Is(err, fault.Invalid) || got != nil {
		t.Fatal(got, err)
	}
	state := &driverState{columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(1), "Ada"}}}
	db, err := database.Open(t.Context(), database.Adapter{Connector: recordConnector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		q := SelectTransaction(source, schema, Map(schema.Name, expr.At(scope, orm.TextExpr(m.Name))), Map(schema.ID, expr.At(scope, orm.KeyExpr(m.ID)))).Limit(2)
		got, err := q.All(t.Context(), tx)
		if err == nil && !reflect.DeepEqual(got, []record{{1, "Ada"}}) {
			t.Error(got)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.closed || !strings.Contains(state.sql, `FOR UPDATE SKIP LOCKED`) {
		t.Fatal(state.sql, state.closed)
	}
	invalid := source
	invalid.core.Transaction = false
	if _, err := SelectTransactionRecord(invalid, scope).All(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}

func TestModelRecordDirectAliasRequiresNoOuterClauses(t *testing.T) {
	m := declareMembers()
	if !ModelRecord(m.Query()).core.Direct {
		t.Fatal("plain model lost direct alias")
	}
	for _, q := range []orm.Query[member]{m.Query().Where(orm.KeyEq(m.ID, 1)), m.Query().OrderBy(orm.KeyAsc(m.ID)), m.Query().Limit(0), m.Query().Offset(1)} {
		if ModelRecord(q).core.Direct {
			t.Fatal("model clause lost its SELECT boundary")
		}
	}
}
