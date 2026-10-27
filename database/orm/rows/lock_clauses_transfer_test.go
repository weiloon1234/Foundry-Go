package rows

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"strings"
	"testing"
)

func TestMultipleTypedLockClausesAndImmutablePolicies(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	j := InnerJoin(a, b, On(expr.At(Scope(a), m.ID), expr.At(Scope(b), m.ID)))
	l, r := Target(LeftScope(j, Scope(a))), Target(RightScope(j, Scope(b)))
	source := ReadRecord(SelectRecord(j, LeftScope(j, Scope(a))))
	locked := LockRows(source, UpdateLock(l), KeyShareLock(r).NoWait())
	statement, err := ormcore.CompileRecord(t.Context(), locked.core)
	if err != nil || !strings.HasSuffix(statement.SQLText, ` FOR UPDATE OF "a" FOR KEY SHARE OF "b" NOWAIT`) {
		t.Fatal(statement, err)
	}
	derived := locked.SkipLocked()
	updated, err := ormcore.CompileRecord(t.Context(), derived.core)
	if err != nil || !strings.HasSuffix(updated.SQLText, ` FOR UPDATE OF "a" SKIP LOCKED FOR KEY SHARE OF "b" SKIP LOCKED`) {
		t.Fatal(updated, err)
	}
	again, err := ormcore.CompileRecord(t.Context(), locked.core)
	if err != nil || again.SQLText != statement.SQLText {
		t.Fatal("policy mutated input", err)
	}
	for _, bad := range []LockedResult[Inner[Alias[left, member], Alias[right, member]], member]{LockRows(source), LockRows(source, RowLock[Inner[Alias[left, member], Alias[right, member]]]{}), LockRows(source, UpdateLock(l, l)), locked.Of(l)} {
		if _, err := ormcore.CompileRecord(t.Context(), bad.core); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid lock compiled", err)
		}
	}
}

func TestLockClauseStrengthsAndOwnedInputs(t *testing.T) {
	m := declareMembers()
	a := As[left](m.Query(), "a")
	target := Target(Scope(a))
	source := ReadRecord(SelectRecord(a, Scope(a)))
	targets := []LockTarget[Alias[left, member]]{target}
	update := UpdateLock(targets...)
	targets[0] = LockTarget[Alias[left, member]]{}
	clauses := []RowLock[Alias[left, member]]{update}
	copied := LockRows(source, clauses...)
	clauses[0] = RowLock[Alias[left, member]]{}
	for _, test := range []struct {
		q      LockedResult[Alias[left, member], member]
		suffix string
	}{
		{copied, ` FOR UPDATE OF "a"`},
		{LockRows(source, NoKeyUpdateLock(target).NoWait()), ` FOR NO KEY UPDATE OF "a" NOWAIT`},
		{LockRows(source, ShareLock(target).SkipLocked()), ` FOR SHARE OF "a" SKIP LOCKED`},
		{LockRows(source, KeyShareLock(target).NoWait().SkipLocked().Wait()), ` FOR KEY SHARE OF "a"`},
		{copied.NoWait().Wait(), ` FOR UPDATE OF "a"`},
	} {
		s, e := ormcore.CompileRecord(t.Context(), test.q.core)
		if e != nil || !strings.HasSuffix(s.SQLText, test.suffix) {
			t.Fatal(s, e)
		}
	}
	scalar := LockRows(ReadScalar(SelectScalar(a, expr.Row(expr.At(Scope(a), m.ID)).Value())), KeyShareLock(target))
	if s, e := ormcore.CompileRecord(t.Context(), scalar.core); e != nil || !strings.HasSuffix(s.SQLText, ` FOR KEY SHARE OF "a"`) {
		t.Fatal(s, e)
	}
	foreign := Target(Scope(As[left](declareMembers().Query(), "unknown")))
	for _, clause := range []RowLock[Alias[left, member]]{UpdateLock[Alias[left, member]](), UpdateLock(LockTarget[Alias[left, member]]{}), UpdateLock(target, target), UpdateLock(foreign)} {
		if _, e := ormcore.CompileRecord(t.Context(), LockRows(source, clause).core); !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
	}
}

func TestLockClausesRejectWindowsInsideCalculations(t *testing.T) {
	m := declareMembers()
	source := From(m.Query())
	id := expr.Value(m.ID)
	rank := expr.RowNumber(expr.WindowFor(ModelScope(m.Query())))
	calculation := expr.AddValue(rank, rank)
	conditional := expr.WhenValue(expr.EqualValue(rank, rank), expr.Row(id).Value()).Else(expr.Row(id).Value())
	for _, q := range []Scalar[member, int64]{SelectScalar(source, calculation), SelectScalar(source, conditional), OrderScalar(SelectScalar(source, expr.Row(id).Value()), calculation.Asc())} {
		read := ReadScalar(q)
		if _, e := CompileRead(read); e != nil {
			t.Fatal(e)
		}
		for _, locked := range []LockedResult[member, int64]{ForUpdate(read), LockRows(read, UpdateLock(Target(ModelScope(m.Query()))))} {
			if _, e := ormcore.CompileRecord(t.Context(), locked.core); !errors.Is(e, fault.Invalid) {
				t.Fatal("hidden window locked", e)
			}
		}
	}
	inner := LimitScalar(SelectScalar(source, calculation), 1)
	outer := ReadScalar(SelectScalar(source, ScalarRowQuery(source, inner).Value()))
	if _, e := ormcore.CompileRecord(t.Context(), LockRows(outer, UpdateLock(Target(ModelScope(m.Query())))).core); e != nil {
		t.Fatal("independent inner window blocked outer lock", e)
	}
}

func TestLockedProjectionScopesAndDerivedSourceValidation(t *testing.T) {
	m := declareMembers()
	a, b := As[left](m.Query(), "a"), As[right](m.Query(), "b")
	j := LeftJoin(a, b, On(expr.At(Scope(a), m.ID), expr.At(Scope(b), m.ID)))
	leftTarget := Target(LeftScope(j, Scope(a)))
	projection := ReadRecord(SelectRecord(j, LeftScope(j, Scope(a))))
	locked := ForUpdate(projection).Of(leftTarget).NoWait()
	s, e := ormcore.CompileRecord(t.Context(), locked.core)
	if e != nil || !strings.HasSuffix(s.SQLText, ` FOR UPDATE OF "a" NOWAIT`) {
		t.Fatal(s, e)
	}
	targets := []LockTarget[Left[Alias[left, member], Alias[right, member]]]{leftTarget}
	copied := ForShare(projection).Of(targets...)
	targets[0] = LockTarget[Left[Alias[left, member], Alias[right, member]]]{}
	if _, e := ormcore.CompileRecord(t.Context(), copied.core); e != nil {
		t.Fatal("target slice retained", e)
	}
	nullable, foreign := leftTarget, leftTarget
	nullable.core.Reference.Table = "b"
	foreign.core.Reference.Table = "elsewhere"
	for _, bad := range []LockedResult[Left[Alias[left, member], Alias[right, member]], member]{ForUpdate(projection), locked.Of(), locked.Of(leftTarget, leftTarget), locked.Of(LockTarget[Left[Alias[left, member], Alias[right, member]]]{}), locked.Of(nullable), locked.Of(foreign)} {
		if _, e := ormcore.CompileRecord(t.Context(), bad.core); !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
	}
	derived := As[left](m.Query().Limit(2), "limited")
	if _, e := ormcore.CompileRecord(t.Context(), ForUpdate(ReadRecord(SelectRecord(derived, Scope(derived)))).Of(Target(Scope(derived))).core); e != nil {
		t.Fatal("ordinary derived lock", e)
	}
	cte := AsRecord[left](CTE("named", ModelRecord(m.Query())), "named_rows")
	read := ReadRecord(SelectRecord(cte, Scope(cte)))
	for _, bad := range []LockedResult[Alias[left, member], member]{ForUpdate(read), ForUpdate(read).Of(Target(Scope(cte)))} {
		if _, e := ormcore.CompileRecord(t.Context(), bad.core); !errors.Is(e, fault.Invalid) {
			t.Fatal("CTE claimed protection", e)
		}
	}
	source := From(m.Query())
	id := expr.Value(m.ID)
	scalar := SelectScalar(source, expr.Row(id).Value())
	for _, q := range []Scalar[member, int64]{DistinctScalar(scalar), GroupByScalar(scalar, expr.Group(id)), SelectScalar(source, expr.Count[member]().Value()), SelectScalar(source, expr.RowNumber(expr.WindowFor(ModelScope(m.Query())).OrderBy(id.Asc())))} {
		if _, e := ormcore.CompileRecord(t.Context(), ForUpdate(ReadScalar(q)).core); !errors.Is(e, fault.Invalid) {
			t.Fatal("non-row projection locked", e)
		}
	}
	combined := UnionAll(ModelRecord(m.Query()), ModelRecord(m.Query()))
	if _, e := ormcore.CompileRecord(t.Context(), ForUpdate(ReadSet(combined)).core); !errors.Is(e, fault.Invalid) {
		t.Fatal("set locked", e)
	}
	distinct := AsRecord[left](DistinctRecord(SelectRecord(a, Scope(a))), "deduplicated")
	if _, e := ormcore.CompileRecord(t.Context(), ForUpdate(ReadRecord(SelectRecord(distinct, Scope(distinct)))).core); !errors.Is(e, fault.Invalid) {
		t.Fatal("derived distinct locked", e)
	}
}
