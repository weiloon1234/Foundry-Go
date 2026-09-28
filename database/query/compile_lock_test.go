package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLockedModelCompilerStrengthsWindowsAndImmutability(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	base := cursorQuery().Where(id.Gt(3)).OrderBy(id.Desc()).Limit(2).Offset(1)
	for _, test := range []struct {
		q      LockedQuery[cursorRecord]
		suffix string
	}{
		{base.ForUpdate(), " FOR UPDATE"},
		{base.ForNoKeyUpdate().SkipLocked(), " FOR NO KEY UPDATE SKIP LOCKED"},
		{base.ForShare().NoWait(), " FOR SHARE NOWAIT"},
		{base.ForKeyShare().NoWait().SkipLocked().Wait(), " FOR KEY SHARE"},
	} {
		s, err := test.q.Compile()
		if err != nil || !strings.HasSuffix(s.SQL(), `LIMIT $2 OFFSET $3`+test.suffix) || !reflect.DeepEqual(s.Arguments(), []any{int64(3), int64(2), int64(1)}) {
			t.Fatal("lock lost selection, parameters or policy", s, err)
		}
	}
	original, err := base.Compile()
	if err != nil || strings.Contains(original.SQL(), " FOR ") {
		t.Fatal("locking mutated ordinary query", err)
	}
	zero := base.Limit(0).ForUpdate()
	zero.query = zero.query.firstQuery()
	s, err := zero.Compile()
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(3), int64(0), int64(1)}) {
		t.Fatal("locked First erased zero limit", s, err)
	}
	for _, q := range []LockedQuery[cursorRecord]{{}, For[cursorRecord]("records").ForUpdate(), base.ForUpdate().Limit(-1)} {
		if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid locked model compiled", err)
		}
	}
}

type lockLeft struct{}
type lockRight struct{}

func TestLockedProjectionScopesAndDerivedSourceValidation(t *testing.T) {
	base := cursorQuery()
	a, b := As[lockLeft](base, "a"), As[lockRight](base, "b")
	af := NewOrderedField[Alias[lockLeft, cursorRecord], int64]("a", "id", codec.Signed[int64]())
	bf := NewOrderedField[Alias[lockRight, cursorRecord], int64]("b", "id", codec.Signed[int64]())
	joined := LeftJoin(a, b, On(af, bf))
	left := LeftScope(joined, a.Scope())
	projection := SelectRecord(joined, left)
	locked := projection.ForUpdate().Of(left).NoWait()
	s, err := locked.Compile()
	if err != nil || !strings.HasSuffix(s.SQL(), ` FOR UPDATE OF "a" NOWAIT`) {
		t.Fatal(s, err)
	}
	targets := []LockTarget[Left[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]]]{left}
	copied := projection.ForShare().Of(targets...)
	targets[0] = nil
	if _, err := copied.Compile(); err != nil {
		t.Fatal("target slice was retained", err)
	}
	for _, invalid := range []LockedResult[Left[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]], cursorRecord]{
		projection.ForUpdate(), locked.Of(), locked.Of(left, left), locked.Of(nil),
		locked.Of(RecordScope[Left[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]], cursorRecord]{table: "b"}),
		locked.Of(RecordScope[Left[Alias[lockLeft, cursorRecord], Alias[lockRight, cursorRecord]], cursorRecord]{table: "elsewhere"}),
	} {
		if _, err := invalid.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("unsafe outer-join lock compiled", err)
		}
	}
	derived := As[lockLeft](base.Limit(2), "limited")
	if _, err := SelectRecord(derived, derived.Scope()).ForUpdate().Of(derived.Scope()).Compile(); err != nil {
		t.Fatal("ordinary derived source cannot lock", err)
	}
	cte := As[lockLeft](CTE("named", base), "named_rows")
	for _, q := range []LockedResult[Alias[lockLeft, cursorRecord], cursorRecord]{
		SelectRecord(cte, cte.Scope()).ForUpdate(), SelectRecord(cte, cte.Scope()).ForUpdate().Of(cte.Scope()),
	} {
		if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("CTE-only lock claimed protection", err)
		}
	}
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	values := SelectValue(base, id.Value())
	for _, q := range []LockedResult[cursorRecord, int64]{
		values.Distinct().ForUpdate(), values.GroupBy(id.Group()).ForUpdate(),
		SelectValue(base, Count[cursorRecord]().Value()).ForUpdate(),
		SelectValue(base, RowNumber(WindowFor(base).OrderBy(id.Asc()))).ForUpdate(),
	} {
		if _, err := q.Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("non-row projection locked", err)
		}
	}
	combined := base.UnionAll(base)
	if _, err := SelectRecord(combined, combined.Scope()).ForUpdate().Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("set result locked", err)
	}
	distinct := As[lockLeft](base.Distinct(), "deduplicated")
	if _, err := SelectRecord(distinct, distinct.Scope()).ForUpdate().Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("derived distinct result locked", err)
	}
}

func TestLockCompilerQualifiedTargetsAndLaterNullableSides(t *testing.T) {
	source := tableSource{table: "domain.records"}
	node := selectNode{source: source}
	s, err := compileLock(node, lockSpec{strength: lockUpdate, explicit: true, targets: []string{"domain.records"}})
	if err != nil || s != ` FOR UPDATE OF "records"` {
		t.Fatal("qualified OF is not a bare relation reference", s, err)
	}
	node.joins = []joinNode{{source: tableSource{table: "other.records"}, kind: innerJoin}}
	if _, err := compileLock(node, lockSpec{strength: lockUpdate, explicit: true, targets: []string{"domain.records"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("ambiguous schema targets compiled", err)
	}
	node.joins = []joinNode{{source: tableSource{table: "other", alias: "b"}, kind: leftJoin}, {source: tableSource{table: "last", alias: "c"}, kind: rightJoin}}
	if _, err := compileLock(node, lockSpec{strength: lockUpdate, explicit: true, targets: []string{"domain.records"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("later RIGHT JOIN did not null-extend earlier side", err)
	}
	if _, err := compileLock(node, lockSpec{strength: lockUpdate, explicit: true, targets: []string{"c"}}); err != nil {
		t.Fatal("preserved RIGHT JOIN side cannot lock", err)
	}
	cteOnly := selectNode{source: tableSource{cte: &cteNode{name: "input"}}}
	node.joins = []joinNode{{source: tableSource{query: &cteOnly, alias: "derived"}, kind: innerJoin}}
	if _, err := compileLock(node, lockSpec{strength: lockUpdate, explicit: true, targets: []string{"domain.records", "derived"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("explicit derived CTE target silently acquired no lock", err)
	}
}
