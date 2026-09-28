package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type transactionTestRecord struct{ ID int64 }
type transactionTestAlias struct{}

func transactionTestModel() Query[transactionTestRecord] {
	return ForModel(Define("records", "id", []Column{{Name: "id"}}, func(row database.Row) (transactionTestRecord, error) {
		var result transactionTestRecord
		err := row.Scan(&result.ID)
		return result, err
	}, NewModelField("id", codec.Signed[int64](), func(v transactionTestRecord) int64 { return v.ID })))
}
func TestTransactionNestedLocksRejectOrdinaryGenericQueries(t *testing.T) {
	base := transactionTestModel()
	inner := base.ForUpdate().transactionRecord()
	type owner = TransactionAlias[transactionTestAlias, transactionTestRecord]
	ordinary := ForModel(Define("records", "id", []Column{{Name: "id"}}, func(row database.Row) (owner, error) {
		var ignored int64
		err := row.Scan(&ignored)
		return owner{}, err
	})).Where(Predicate[owner]{expression: subqueryPredicate{query: subquery{node: inner.node}}})
	for _, kind := range []readKind{readModels, readCount, readExists} {
		statement, err := ordinary.compile(kind)
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") || statement.SQL() != "" {
			t.Fatal("ordinary generic query accepted nested lock", kind, statement, err)
		}
	}
	source := AsTransaction[transactionTestAlias](TransactionCTE("claimed", base.ForShare()), "c")
	escaped := SelectRecord(transactionProjection(source), source.Scope())
	if _, err := escaped.Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") {
		t.Fatal("ordinary projection accepted private transaction adapter", err)
	}
}
func TestTransactionCTEPreservesInnerLocksAndOuterAggregates(t *testing.T) {
	base := transactionTestModel()
	source := AsTransaction[transactionTestAlias](TransactionCTE("claimed", base.Limit(2).ForUpdate().SkipLocked()), "c")
	q := SelectTransactionRecord(source, source.Scope())
	statement, err := q.Compile()
	if err != nil || !strings.Contains(statement.SQL(), "LIMIT $1 FOR UPDATE SKIP LOCKED)") || strings.HasSuffix(statement.SQL(), "SKIP LOCKED") {
		t.Fatal("lock moved outside owning CTE", statement, err)
	}
	field := NewProjectionField[int64, int64]("count")
	definition := DefineProjection([]ProjectionColumn[int64]{field.Column()}, func(row database.Row) (int64, error) {
		var total int64
		err := row.Scan(&total)
		return total, err
	})
	aggregate := ProjectTransaction(source, definition, Map(field, Count[TransactionAlias[transactionTestAlias, transactionTestRecord]]().Value()))
	statement, err = aggregate.Compile()
	if err != nil || !strings.Contains(statement.SQL(), "FOR UPDATE SKIP LOCKED)") || !strings.Contains(statement.SQL(), "COUNT(*)") {
		t.Fatal("outer aggregate lost inner lock scope", statement, err)
	}
	if _, err := q.ForUpdate().Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("outer lock claimed to protect CTE references", err)
	}
}
func TestTransactionLockValidationSharesStatementBudget(t *testing.T) {
	node := selectNode{source: tableSource{table: "records"}}
	var walk selectWalk
	walk.selectNode(node, 0)
	specs := make([]lockSpec, MaxExpressionNodes/walk.nodes)
	for i := range specs {
		specs[i] = lockSpec{strength: lockUpdate}
	}
	var c compiler
	if _, err := c.compileLocks(node, specs); err != nil {
		t.Fatal("first bounded lock work failed", err)
	}
	if _, err := c.compileLocks(node, specs[:1]); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "nested") {
		t.Fatal("statement lock work was reset", err)
	}
}
