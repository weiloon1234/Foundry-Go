package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestMultipleTypedLockClausesAndImmutablePolicies(t *testing.T) {
	base := cursorQuery()
	a, b := As[lockLeft](base, "a"), As[lockRight](base, "b")
	af := NewOrderedField[Alias[lockLeft, cursorRecord], int64]("a", "id", codec.Signed[int64]())
	bf := NewOrderedField[Alias[lockRight, cursorRecord], int64]("b", "id", codec.Signed[int64]())
	joined := InnerJoin(a, b, On(af, bf))
	left, right := LeftScope(joined, a.Scope()), RightScope(joined, b.Scope())
	source := SelectRecord(joined, left)
	locked := source.LockRows(UpdateLock(left), KeyShareLock(right).NoWait())
	statement, err := locked.Compile()
	if err != nil || !strings.HasSuffix(statement.SQL(), ` FOR UPDATE OF "a" FOR KEY SHARE OF "b" NOWAIT`) {
		t.Fatal(statement, err)
	}
	derived := locked.SkipLocked()
	updated, err := derived.Compile()
	if err != nil || !strings.HasSuffix(updated.SQL(), ` FOR UPDATE OF "a" SKIP LOCKED FOR KEY SHARE OF "b" SKIP LOCKED`) {
		t.Fatal(updated, err)
	}
	again, err := locked.Compile()
	if err != nil || again.SQL() != statement.SQL() {
		t.Fatal("wait policy mutated original clauses", err)
	}
	for _, invalid := range []LockedResult[Inner[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]], cursorRecord]{
		source.LockRows(), source.LockRows(RowLock[Inner[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]]]{}),
		source.LockRows(UpdateLock(left, left)), locked.Of(left),
	} {
		if _, err := invalid.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid lock clauses compiled", err)
		}
	}
}

func TestLockClauseStrengthsAndOwnedInputs(t *testing.T) {
	source := As[lockLeft](cursorQuery(), "a")
	scope := source.Scope()
	selected := SelectRecord(source, scope)
	targets := []LockTarget[Alias[lockLeft, cursorRecord]]{scope}
	update := UpdateLock(targets...)
	targets[0] = nil
	clauses := []RowLock[Alias[lockLeft, cursorRecord]]{update}
	copied := selected.LockRows(clauses...)
	clauses[0] = RowLock[Alias[lockLeft, cursorRecord]]{}
	for _, test := range []struct {
		query  LockedResult[Alias[lockLeft, cursorRecord], cursorRecord]
		suffix string
	}{
		{copied, ` FOR UPDATE OF "a"`},
		{selected.LockRows(NoKeyUpdateLock(scope).NoWait()), ` FOR NO KEY UPDATE OF "a" NOWAIT`},
		{selected.LockRows(ShareLock(scope).SkipLocked()), ` FOR SHARE OF "a" SKIP LOCKED`},
		{selected.LockRows(KeyShareLock(scope).NoWait().SkipLocked().Wait()), ` FOR KEY SHARE OF "a"`},
		{copied.NoWait().Wait(), ` FOR UPDATE OF "a"`},
	} {
		s, err := test.query.Compile()
		if err != nil || !strings.HasSuffix(s.SQL(), test.suffix) {
			t.Fatal(s, err)
		}
	}
	field := NewOrderedField[Alias[lockLeft, cursorRecord], int64]("a", "id", codec.Signed[int64]())
	values := SelectValue(source, field.Value()).LockRows(KeyShareLock(scope))
	if s, err := values.Compile(); err != nil || !strings.HasSuffix(s.SQL(), ` FOR KEY SHARE OF "a"`) {
		t.Fatal(s, err)
	}
	var nilScope *RecordScope[Alias[lockLeft, cursorRecord], cursorRecord]
	for _, clause := range []RowLock[Alias[lockLeft, cursorRecord]]{
		UpdateLock[Alias[lockLeft, cursorRecord]](),
		UpdateLock[Alias[lockLeft, cursorRecord]](nil),
		UpdateLock[Alias[lockLeft, cursorRecord]](nilScope),
		UpdateLock(scope, scope),
		UpdateLock(RecordScope[Alias[lockLeft, cursorRecord], cursorRecord]{table: "unknown"}),
	} {
		if _, err := selected.LockRows(clause).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid clause compiled", err)
		}
	}
}

func TestLockClausesRejectNullableTargetsAndBoundCombinedWork(t *testing.T) {
	base := cursorQuery()
	a, b := As[lockLeft](base, "a"), As[lockRight](base, "b")
	af := NewOrderedField[Alias[lockLeft, cursorRecord], int64]("a", "id", codec.Signed[int64]())
	bf := NewOrderedField[Alias[lockRight, cursorRecord], int64]("b", "id", codec.Signed[int64]())
	joined := LeftJoin(a, b, On(af, bf))
	left := LeftScope(joined, a.Scope())
	forged := RecordScope[Left[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]], cursorRecord]{table: "b"}
	if _, err := SelectRecord(joined, left).LockRows(UpdateLock(left), ShareLock(forged)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nullable clause reached SQL", err)
	}
	node := selectNode{source: tableSource{table: "records"}}
	var walk selectWalk
	walk.selectNode(node, 0)
	if walk.err != nil || walk.nodes < 2 {
		t.Fatal("invalid budget fixture", walk)
	}
	specs := make([]lockSpec, MaxExpressionNodes/walk.nodes+1)
	for i := range specs {
		specs[i] = lockSpec{strength: lockUpdate}
	}
	if _, err := (&compiler{}).compileLocks(node, specs[:len(specs)-1]); err != nil {
		t.Fatal("bounded clauses failed", err)
	}
	if _, err := (&compiler{}).compileLocks(node, specs); !errors.Is(err, fault.Invalid) {
		t.Fatal("combined lock work unbounded", err)
	}
	if _, err := (&compiler{}).compileLocks(node, make([]lockSpec, MaxExpressionNodes+1)); !errors.Is(err, fault.Invalid) {
		t.Fatal("clause count unbounded", err)
	}
	if _, err := (&compiler{}).compileLocks(node, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty clauses compiled", err)
	}
}

func TestLockClausesRejectWindowsInsideCalculations(t *testing.T) {
	base := cursorQuery()
	rank := RowNumber(WindowFor(base))
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	calculation := AddValue(rank, rank)
	conditional := WhenValue(EqualValue(rank, rank), id.Value()).Else(id.Value())
	for _, selected := range []ValueQuery[cursorRecord, int64]{
		SelectValue(base, calculation), SelectValue(base, conditional),
		SelectValue(base, id.Value()).OrderBy(calculation.Asc()),
	} {
		if _, err := selected.Compile(); err != nil {
			t.Fatal("ordinary window calculation failed", err)
		}
		for _, locked := range []LockedResult[cursorRecord, int64]{
			selected.ForUpdate(), selected.LockRows(UpdateLock(base.Scope())),
		} {
			if _, err := locked.Compile(); !errors.Is(err, fault.Invalid) {
				t.Fatal("window calculation was accepted as a lock target", err)
			}
		}
	}
	inner := SelectValue(base, calculation).Limit(1)
	outer := SelectValue(base, ScalarQuery(base, inner)).LockRows(UpdateLock(base.Scope()))
	if _, err := outer.Compile(); err != nil {
		t.Fatal("independent inner window prevented outer row locking", err)
	}
}
