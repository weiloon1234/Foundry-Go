package rows

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
	"strings"
	"testing"
)

func TestTransactionCTEPreservesInnerLocksAndOuterAggregates(t *testing.T) {
	m := declareMembers()
	a := AsTransaction[left](TransactionCTE("claimed", LockedRecord(orm.Lock(m.Query().Limit(2)).SkipLocked())), "c")
	r := SelectTransactionRecord(a, TransactionScope(a))
	sql := compileTransaction(t, r.core.Source)
	if !strings.Contains(sql, "LIMIT $1 FOR UPDATE SKIP LOCKED)") || strings.HasSuffix(sql, "SKIP LOCKED") {
		t.Fatal(sql)
	}
	sql = compileTransaction(t, SelectTransactionValue(a, expr.AsSelected(expr.Count[TransactionAlias[left, member]]())).core.Source)
	if !strings.Contains(sql, "COUNT(*)") || !strings.Contains(sql, "FOR UPDATE SKIP LOCKED)") {
		t.Fatal(sql)
	}
	bad := r
	bad.core.Source.Node.Locks = []querycore.LockSpec{{Strength: querycore.LockUpdate}}
	if _, e := querycore.CompileRead(bad.core.Source.Node, t.Context(), true, bad.core.Source.Err, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("outer lock on CTE", e)
	}
	if _, e := querycore.CompileRead(r.core.Source.Node, t.Context(), false, r.core.Source.Err, true); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "transaction-scoped") {
		t.Fatal("ordinary CTE adapter", e)
	}
}
func TestTransactionCorrelatedSourceValidationAndImmutability(t *testing.T) {
	m := declareMembers()
	a, b := AsTransaction[left](TransactionOf(ModelRecord(m.Query())), "parent"), AsTransaction[right](TransactionOf(ModelRecord(m.Query())), "child")
	c := TransactionCorrelate(a, b)
	inner, outer := TransactionInnerScope(c, TransactionScope(b)), TransactionOuterScope(c, TransactionScope(a))
	field := expr.At(inner, orm.KeyExpr(m.ID))
	base := SelectTransactionCorrelatedScalar(c, expr.AsSelected(expr.Row(field)))
	locked := base.ForUpdate().Of(Target(inner))
	q := SelectTransactionRecord(a, TransactionScope(a))
	before := compileTransaction(t, q.Where(locked.Exists()).core.Source)
	changed := locked.SkipLocked().Where(expr.Greater(field, expr.ParamOf(expr.Row(field), 3)))
	if sql := compileTransaction(t, q.Where(changed.Exists()).core.Source); !strings.Contains(sql, "SKIP LOCKED") {
		t.Fatal(sql)
	}
	after := compileTransaction(t, q.Where(locked.Exists()).core.Source)
	if before != after {
		t.Fatal("correlated policy mutated")
	}
	for _, bad := range []TransactionCorrelatedScalar[TransactionAlias[left, member], TransactionAlias[right, member], int64]{base.NoWait(), base.LockRows(), locked.Of(Target(outer)), locked.Of()} {
		s := q.Where(bad.Exists()).core.Source
		if _, e := querycore.CompileRead(s.Node, t.Context(), true, s.Err, true); !errors.Is(e, fault.Invalid) {
			t.Fatal("invalid correlated lock", e)
		}
	}
	shadow := TransactionCorrelate(a, AsTransaction[right](TransactionOf(ModelRecord(m.Query())), "parent"))
	s := q.Where(shadow.Exists()).core.Source
	if _, e := querycore.CompileRead(s.Node, t.Context(), true, s.Err, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("shadow", e)
	}
	var missing TransactionCorrelatedRecord[TransactionAlias[left, member], TransactionAlias[right, member], member]
	lat := AsTransactionLateral[latestTag](missing, "bad")
	j := TransactionCrossJoinLateral(a, lat)
	s = SelectTransactionRecord(j, LeftScope(j, TransactionScope(a))).core.Source
	if _, e := querycore.CompileRead(s.Node, t.Context(), true, s.Err, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("nil lateral", e)
	}
	validLateral := AsTransactionLateral[latestTag](SelectTransactionCorrelatedRecord(c, inner).ForUpdate(), "valid_lateral")
	lateralJoin := TransactionCrossJoinLateral(a, validLateral)
	lateralSource := SelectTransactionRecord(lateralJoin, LeftScope(lateralJoin, TransactionScope(a))).core.Source
	compileTransaction(t, lateralSource)
	if _, err := querycore.CompileRead(lateralSource.Node, t.Context(), false, lateralSource.Err, true); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") {
		t.Fatal("LATERAL escaped transaction", err)
	}
	// The same correlated SQL must never escape through ordinary model/read shapes.
	for _, s := range []querycore.SelectNode{q.Where(locked.Exists()).core.Source.Node, SelectTransactionValue(a, expr.AsSelected(TransactionCorrelatedScalarRowQuery(locked.Limit(1)))).core.Source.Node} {
		if _, e := querycore.CompileRead(s, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "transaction-scoped") {
			t.Fatal("correlated lock escaped", e)
		}
	}
	compileTransaction(t, SelectTransactionValue(a, expr.AsSelected(TransactionCorrelatedScalarRowQuery(locked.Limit(1)))).core.Source)
}
func TestTransactionComputedMembershipQualificationAndBindings(t *testing.T) {
	m := declareMembers()
	a := AsTransaction[left](TransactionOf(ModelRecord(m.Query())), "outer_records")
	f := expr.At(TransactionScope(a), orm.KeyExpr(m.ID))
	inner := TransactionValueOf(Values(WhereScalar(SelectScalar(From(m.Query()), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(m.ID))))), expr.Greater(expr.Value(orm.KeyExpr(m.ID)), expr.Param(orm.KeyExpr(m.ID), 9)))))
	p := TransactionInQuery(expr.Add(f, expr.ParamOf(expr.Row(f), 7)), inner)
	q := SelectTransactionRecord(a, TransactionScope(a)).Where(p)
	s, e := querycore.CompileRead(q.core.Source.Node, t.Context(), true, q.core.Source.Err, true)
	if e != nil || !reflect.DeepEqual(s.Bindings, []any{int64(7), int64(9)}) {
		t.Fatal(s.Bindings, e)
	}
	original := expr.FoundryPredicate(p, sqlowner.Expressions()).Node
	changed := querycore.Requalify(original, "renamed")
	for _, test := range []struct {
		p    querycore.Expression
		name string
	}{{original, "outer_records"}, {changed, "renamed"}} {
		var fields []string
		w := querycore.SelectWalk{LocalSelect: true, Field: func(f querycore.FieldRef) { fields = append(fields, f.Table) }}
		w.Expression(test.p, 0)
		if w.Err != nil || !reflect.DeepEqual(fields, []string{test.name}) {
			t.Fatal(fields, w.Err)
		}
	}
	if changed.(querycore.SubqueryPredicate).Query.Node.Source.Table != "members" {
		t.Fatal("inner select renamed")
	}
	again, e := querycore.CompileRead(q.core.Source.Node, t.Context(), true, q.core.Source.Err, true)
	if e != nil || !reflect.DeepEqual(s, again) {
		t.Fatal("original mutated", e)
	}
	bad := q.Where(TransactionInQuery[TransactionAlias[left, member], int64](nil, inner))
	if _, e := querycore.CompileRead(bad.core.Source.Node, t.Context(), true, bad.core.Source.Err, true); !errors.Is(e, fault.Invalid) {
		t.Fatal("missing IN became EXISTS", e)
	}
	b := AsTransaction[right](LockedRecord(orm.Lock(m.Query())), "inner_records")
	locked := SelectTransactionValue(b, expr.AsSelected(expr.Row(expr.At(TransactionScope(b), orm.KeyExpr(m.ID)))))
	for _, s := range []querycore.SelectNode{SelectTransactionValue(a, expr.AsSelected(TransactionScalarRowQuery(a, locked))).core.Source.Node, q.Where(TransactionInQuery(f, locked)).core.Source.Node, q.Where(TransactionExistsQuery(a, locked)).core.Source.Node} {
		if _, e := querycore.CompileRead(s, t.Context(), false, nil, true); !errors.Is(e, fault.Invalid) || !strings.Contains(e.Error(), "transaction-scoped") {
			t.Fatal("ordinary locked value", e)
		}
		if _, e := querycore.CompileRead(s, t.Context(), true, nil, true); e != nil {
			t.Fatal("valid transaction", e)
		}
	}
}
