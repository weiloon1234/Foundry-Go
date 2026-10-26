package querycore

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

func TestTransactionLockValidationSharesStatementBudget(t *testing.T) {
	node := SelectNode{Source: TableSource{Table: "records"}}
	var walk SelectWalk
	walk.SelectNode(node, 0)
	specs := make([]LockSpec, MaxExpressionNodes/walk.Nodes)
	for i := range specs {
		specs[i] = LockSpec{Strength: LockUpdate}
	}
	var c Compiler
	if _, err := c.CompileLocks(node, specs); err != nil {
		t.Fatal("first bounded lock work failed", err)
	}
	if _, err := c.CompileLocks(node, specs[:1]); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "nested") {
		t.Fatal("statement lock work was reset", err)
	}
}

func TestLockClausesBoundCombinedWork(t *testing.T) {
	node := SelectNode{Source: TableSource{Table: "records"}}
	var walk SelectWalk
	walk.SelectNode(node, 0)
	if walk.Err != nil || walk.Nodes < 2 {
		t.Fatal(walk)
	}
	specs := make([]LockSpec, MaxExpressionNodes/walk.Nodes+1)
	for i := range specs {
		specs[i] = LockSpec{Strength: LockUpdate}
	}
	if _, err := (&Compiler{}).CompileLocks(node, specs[:len(specs)-1]); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]LockSpec{specs, make([]LockSpec, MaxExpressionNodes+1), nil} {
		if _, err := (&Compiler{}).CompileLocks(node, invalid); !errors.Is(err, fault.Invalid) {
			t.Fatal("unbounded or empty lock clauses accepted", err)
		}
	}
}
