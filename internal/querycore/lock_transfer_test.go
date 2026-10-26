package querycore

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestLockCompilerQualifiedTargetsAndLaterNullableSides(t *testing.T) {
	source := TableSource{Table: "domain.records"}
	node := SelectNode{Source: source}
	s, err := CompileLock(node, LockSpec{Strength: LockUpdate, Explicit: true, Targets: []string{"domain.records"}})
	if err != nil || s != ` FOR UPDATE OF "records"` {
		t.Fatal("qualified OF is not a bare relation reference", s, err)
	}
	node.Joins = []JoinNode{{Source: TableSource{Table: "other.records"}, Kind: InnerJoin}}
	if _, err := CompileLock(node, LockSpec{Strength: LockUpdate, Explicit: true, Targets: []string{"domain.records"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("ambiguous schema targets compiled", err)
	}
	node.Joins = []JoinNode{{Source: TableSource{Table: "other", Alias: "b"}, Kind: LeftJoin}, {Source: TableSource{Table: "last", Alias: "c"}, Kind: RightJoin}}
	if _, err := CompileLock(node, LockSpec{Strength: LockUpdate, Explicit: true, Targets: []string{"domain.records"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("later RIGHT JOIN did not null-extend earlier side", err)
	}
	if _, err := CompileLock(node, LockSpec{Strength: LockUpdate, Explicit: true, Targets: []string{"c"}}); err != nil {
		t.Fatal("preserved RIGHT JOIN side cannot lock", err)
	}
	cteOnly := SelectNode{Source: TableSource{Cte: &CteNode{Name: "input"}}}
	node.Joins = []JoinNode{{Source: TableSource{Query: &cteOnly, Alias: "derived"}, Kind: InnerJoin}}
	if _, err := CompileLock(node, LockSpec{Strength: LockUpdate, Explicit: true, Targets: []string{"domain.records", "derived"}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("explicit derived CTE target silently acquired no lock", err)
	}
}

func TestLockClausesRejectNullableTargets(t *testing.T) {
	node := SelectNode{Source: TableSource{Table: "records", Alias: "a"}, Joins: []JoinNode{{Kind: LeftJoin, Source: TableSource{Table: "records", Alias: "b"}, On: BinaryComparison{Left: FieldRef{Table: "a", Column: "id"}, Right: FieldRef{Table: "b", Column: "id"}, Operator: Equal}}}}
	if _, e := (&Compiler{}).CompileLocks(node, []LockSpec{{Strength: LockUpdate, Explicit: true, Targets: []string{"a"}}}); e != nil {
		t.Fatal("preserved side cannot lock", e)
	}
	if _, e := (&Compiler{}).CompileLocks(node, []LockSpec{{Strength: LockUpdate, Explicit: true, Targets: []string{"a"}}, {Strength: LockShare, Explicit: true, Targets: []string{"b"}}}); !errors.Is(e, fault.Invalid) {
		t.Fatal("nullable second clause claimed row protection", e)
	}
}
