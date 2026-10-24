package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func compileTransaction(t *testing.T, s ormcore.Source) string {
	t.Helper()
	q, err := querycore.CompileRead(s.Node, t.Context(), true, s.Err, true)
	if err != nil {
		t.Fatal(err)
	}
	return q.SQLText
}

type transactionTableMarker[S TransactionScopeType] interface{ transactionJoinTable(*S) }
type promotedTransactionAlias struct {
	TransactionAliased[left, member]
}

func TestTransactionJoinsRetainBoundariesAndNullability(t *testing.T) {
	m := declareMembers()
	a := AsTransaction[left](LockedRecord(m.Query().Where(m.ID.Ne(0)).Limit(2).ForUpdate().SkipLocked()), "a")
	b := AsTransaction[right](TransactionOf(ModelRecord(m.Query().Limit(3))), "b")
	as, bs := TransactionScope(a), TransactionScope(b)
	on := On(expr.At(as, m.ID), expr.At(bs, m.ID))
	l := TransactionLeftJoin(a, b, on)
	r := TransactionRightJoin(a, b, on)
	f := TransactionFullJoin(a, b, on)
	schema := outerSchema()
	queries := []ormcore.Source{
		SelectTransactionRecord(TransactionInnerJoin(a, b, on), LeftScope(TransactionInnerJoin(a, b, on), as)).core.Source,
		SelectTransactionRecord(TransactionCrossJoin(a, b), RightScope(TransactionCrossJoin(a, b), bs)).core.Source,
		SelectTransaction(l, schema, Map(schema.ID, expr.AtNullable(NullableRightScope(l, bs), m.ID)), Map(schema.Nickname, expr.AtNullableValue(NullableRightScope(l, bs), m.Nickname))).core.Source,
		SelectTransaction(r, schema, Map(schema.ID, expr.AtNullable(NullableLeftScope(r, as), m.ID)), Map(schema.Nickname, expr.AtNullableValue(NullableLeftScope(r, as), m.Nickname))).core.Source,
		SelectTransaction(f, schema, Map(schema.ID, expr.AtNullable(NullableLeftScope(f, as), m.ID)), Map(schema.Nickname, expr.AtNullableValue(NullableRightScope(f, bs), m.Nickname))).core.Source,
	}
	for _, s := range queries {
		sql := compileTransaction(t, s)
		if !strings.Contains(sql, `FOR UPDATE SKIP LOCKED) AS "a"`) || !strings.Contains(sql, `LIMIT $`) {
			t.Fatal(sql)
		}
		if _, err := querycore.CompileRead(s.Node, t.Context(), false, s.Err, true); !errors.Is(err, fault.Invalid) {
			t.Fatal("nested lock escaped transaction", err)
		}
	}
	c := AsTransaction[latestTag](TransactionOf(ModelRecord(m.Query())), "c")
	chain := TransactionCrossJoin(l, c)
	_ = LeftNullableScope(chain, NullableRightScope(l, bs))
	wrong := AsTransaction[left](TransactionOf(ModelRecord(declareMembers().Query())), "a")
	bad := TransactionInnerJoin(wrong, b, on)
	if !errors.Is(bad.state.source.Err, fault.Invalid) {
		t.Fatal("foreign declaration accepted")
	}
	// Eligibility follows the returned source metadata, even with borrowed markers.
	broken := a
	broken.core.Source.Node.Limit = value.Set(1)
	wrapped := struct {
		transactionTableMarker[TransactionAlias[left, member]]
		TransactionAliased[left, member]
	}{TransactionAliased: broken}
	if j := TransactionLeftJoin(wrapped, b, on); !errors.Is(j.state.source.Err, fault.Invalid) {
		t.Fatal("unwrapped source accepted")
	}
	if j := TransactionRightJoin(wrapped, b, on); !errors.Is(j.state.source.Err, fault.Invalid) {
		t.Fatal("unwrapped source accepted")
	}
	promoted := struct {
		transactionTableMarker[TransactionAlias[left, member]]
		promotedTransactionAlias
	}{promotedTransactionAlias: promotedTransactionAlias{broken}}
	if j := TransactionLeftJoin(b, promoted, On(expr.At(bs, m.ID), expr.At(as, m.ID))); !errors.Is(j.state.source.Err, fault.Invalid) {
		t.Fatal("promoted source accepted")
	}
}

func TestTransactionCorrelationAndLateralLocks(t *testing.T) {
	m := declareMembers()
	a := AsTransaction[left](TransactionOf(ModelRecord(m.Query())), "a")
	b := AsTransaction[right](TransactionOf(ModelRecord(m.Query())), "b")
	c := TransactionCorrelate(a, b)
	outer, inner := TransactionOuterScope(c, TransactionScope(a)), TransactionInnerScope(c, TransactionScope(b))
	x, y := expr.At(outer, m.ID), expr.At(inner, m.ID)
	c = c.Where(expr.Less(y, x))
	record := SelectTransactionCorrelatedRecord(c, inner).OrderBy(y.Desc()).Limit(1).ForUpdate().Of(Target(inner)).SkipLocked()
	lat := AsTransactionLateral[latestTag](record, "latest")
	j := TransactionLeftJoinLateral(a, lat)
	sql := compileTransaction(t, SelectTransactionRecord(j, LeftScope(j, TransactionScope(a))).core.Source)
	for _, want := range []string{`LEFT JOIN LATERAL`, `"b"."id" < "a"."id"`, `FOR UPDATE OF "b" SKIP LOCKED`, `ON TRUE`} {
		if !strings.Contains(sql, want) {
			t.Fatal(sql)
		}
	}
	if len(c.core.Source.Node.Locks) != 0 {
		t.Fatal("lock mutated input")
	}
	if !errors.Is(SelectTransactionCorrelatedRecord(c, inner).NoWait().core.Correlation.Source.Err, fault.Invalid) {
		t.Fatal("missing lock accepted")
	}
	if !errors.Is(SelectTransactionCorrelatedRecord(c, outer).core.Correlation.Source.Err, fault.Invalid) {
		t.Fatal("outer record selected as inner")
	}
	wrong := AsTransaction[left](TransactionOf(ModelRecord(declareMembers().Query())), "a")
	if v := TransactionCrossJoinLateral(wrong, lat); !errors.Is(v.state.source.Err, fault.Invalid) {
		t.Fatal("foreign lateral parent")
	}
	scalar := SelectTransactionCorrelatedScalar(c, expr.Select(y)).Limit(1).ForShare().NoWait()
	out := SelectTransactionValue(a, TransactionCorrelatedScalarQuery(scalar)).Where(scalar.Exists(), TransactionInCorrelatedQuery(expr.At(TransactionScope(a), m.ID), scalar))
	sql = compileTransaction(t, out.core.Source)
	if !strings.Contains(sql, `FOR SHARE NOWAIT`) || !strings.Contains(sql, `EXISTS(SELECT`) {
		t.Fatal(sql)
	}
	// A nested correlation can reference an enclosing correlation, but not execute alone.
	d := AsTransaction[latestTag](TransactionOf(ModelRecord(m.Query())), "d")
	nested := TransactionCorrelate(c, d)
	nestedScope := TransactionOuterScope(nested, inner)
	nestedID := expr.At(nestedScope, m.ID)
	childID := expr.At(TransactionInnerScope(nested, TransactionScope(d)), m.ID)
	c = c.Where(nested.Where(expr.Equal(childID, nestedID)).Exists())
	compileTransaction(t, SelectTransactionRecord(a, TransactionScope(a)).Where(c.Exists()).core.Source)
	totals := lateralTotalSchema()
	mapped := ProjectTransactionCorrelated(c, totals, MapSelected(totals.Count, expr.Count[TransactionCorrelation[TransactionAlias[left, member], TransactionAlias[right, member]]]().Value()))
	projected := AsTransactionLateral[latestTag](mapped, "counts")
	cross := TransactionCrossJoinLateral(a, projected)
	compileTransaction(t, SelectTransactionRecord(cross, RightScope(cross, TransactionLateralScope(projected))).core.Source)
}

func TestTransactionSubqueriesUseCallerTransaction(t *testing.T) {
	m := declareMembers()
	a := AsTransaction[left](TransactionOf(ModelRecord(m.Query())), "a")
	b := AsTransaction[right](TransactionOf(ModelRecord(m.Query())), "b")
	id := expr.At(TransactionScope(a), m.ID)
	selected := SelectTransactionValue(b, expr.Select(expr.At(TransactionScope(b), m.ID))).Limit(1)
	scalar := TransactionScalarOf(selected).ForShare().SkipLocked()
	q := SelectTransactionValue(a, TransactionScalarQuery(a, scalar)).Where(TransactionInQuery(id, scalar), TransactionExistsQuery(a, scalar))
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {int64(7)}}}
	db, err := database.Open(t.Context(), database.Adapter{Connector: recordConnector{state}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	err = db.Transaction(t.Context(), func(tx *database.Tx) error {
		got, err := q.All(t.Context(), tx)
		if err == nil && !reflect.DeepEqual(got, []value.Nullable[int64]{value.Null[int64](), value.Of(int64(7))}) {
			t.Error(got)
		}
		return err
	})
	if err != nil || !strings.Contains(state.sql, `FOR SHARE SKIP LOCKED`) {
		t.Fatal(state.sql, err)
	}
	if got, err := q.All(t.Context(), nil); !errors.Is(err, fault.Invalid) || got != nil {
		t.Fatal(got, err)
	}
	nullable := SelectTransactionValue(b, expr.Select(expr.At(TransactionScope(b), m.Nickname)))
	compileTransaction(t, SelectTransactionValue(a, TransactionScalarNullableQuery(a, nullable)).core.Source)
	_ = TransactionScalarNullableRowQuery(a, nullable)
	_ = TransactionScalarRowQuery(a, selected)
	empty := TransactionScalarOf(TransactionValue[TransactionAlias[right, member], int64]{})
	if !errors.Is(empty.core.Source.Err, fault.Invalid) {
		t.Fatal("zero scalar accepted")
	}
	if !errors.Is(TransactionScalarOf(selected).NoWait().core.Source.Err, fault.Invalid) {
		t.Fatal("missing lock policy accepted")
	}
}
