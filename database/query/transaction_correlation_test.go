package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type transactionCorrelationInner struct{}
type transactionCorrelationLateral struct{}

func TestTransactionCorrelatedLocksRejectOrdinaryExecution(t *testing.T) {
	type outerOwner = TransactionAlias[transactionTestAlias, transactionTestRecord]
	type innerOwner = TransactionAlias[transactionCorrelationInner, transactionTestRecord]
	type pair = TransactionCorrelation[outerOwner, innerOwner]
	parent := AsTransaction[transactionTestAlias](TransactionOf(transactionTestModel()), "outer_records")
	child := AsTransaction[transactionCorrelationInner](TransactionOf(transactionTestModel()), "child")
	c := TransactionCorrelate(parent, child)
	field := NewOrderedField[pair, int64]("child", "id", codec.Signed[int64]())
	values := SelectTransactionCorrelatedValue(c, field.Value()).ForUpdate()
	ordinary := ForModel(Define("outer_records", "id", []Column{{Name: "id"}}, func(row database.Row) (outerOwner, error) {
		var ignored int64
		err := row.Scan(&ignored)
		return outerOwner{}, err
	}))
	for _, kind := range []readKind{readModels, readCount, readExists} {
		statement, err := ordinary.Where(values.Exists()).compile(kind)
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") || statement.SQL() != "" {
			t.Fatal("ordinary query accepted correlated lock", kind, statement, err)
		}
	}
	expression := TransactionCorrelatedScalarQuery(values.Limit(1))
	if _, err := SelectValue(ordinary, expression).Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") {
		t.Fatal("ordinary scalar accepted correlated lock", err)
	}
	if _, err := SelectTransactionValue(parent, expression).Compile(); err != nil {
		t.Fatal("valid correlated scalar failed", err)
	}
	record := SelectTransactionCorrelatedRecord(c, InnerScope(c, child.Scope())).ForShare()
	lateral := AsTransactionLateral[transactionCorrelationLateral](record, "selected_child")
	joined := TransactionLeftJoinLateral(parent, lateral)
	escaped := SelectRecord(transactionProjection(joined), LeftScope(joined, parent.Scope()))
	if _, err := escaped.Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") {
		t.Fatal("ordinary lateral adapter accepted inner lock", err)
	}
}

func TestTransactionCorrelatedSourceValidationAndImmutability(t *testing.T) {
	type outerOwner = TransactionAlias[transactionTestAlias, transactionTestRecord]
	type innerOwner = TransactionAlias[transactionCorrelationInner, transactionTestRecord]
	type pair = TransactionCorrelation[outerOwner, innerOwner]
	parent := AsTransaction[transactionTestAlias](TransactionOf(transactionTestModel()), "parent")
	child := AsTransaction[transactionCorrelationInner](TransactionOf(transactionTestModel()), "child")
	c := TransactionCorrelate(parent, child)
	field := NewOrderedField[pair, int64]("child", "id", codec.Signed[int64]())
	base := SelectTransactionCorrelatedValue(c, field.Value())
	locked := base.ForUpdate().Of(InnerScope(c, child.Scope()))
	first, err := SelectTransactionRecord(parent, parent.Scope()).Where(locked.Exists()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	changed := locked.SkipLocked().Where(field.Gt(3))
	after, err := SelectTransactionRecord(parent, parent.Scope()).Where(changed.Exists()).Compile()
	if err != nil || !strings.Contains(after.SQL(), "SKIP LOCKED") {
		t.Fatal(after, err)
	}
	unchanged, err := SelectTransactionRecord(parent, parent.Scope()).Where(locked.Exists()).Compile()
	if err != nil || unchanged.SQL() != first.SQL() || len(unchanged.Arguments()) != 0 {
		t.Fatal("correlated policy mutated source", unchanged, err)
	}
	for _, bad := range []TransactionCorrelatedValueQuery[outerOwner, innerOwner, int64]{
		base.NoWait(), base.LockRows(), locked.Of(OuterScope(c, parent.Scope())), locked.Of(),
	} {
		if _, err := SelectTransactionRecord(parent, parent.Scope()).Where(bad.Exists()).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid correlated lock lost error", err)
		}
	}
	shadow := TransactionCorrelate(parent, AsTransaction[transactionCorrelationInner](TransactionOf(transactionTestModel()), "parent"))
	if _, err := SelectTransactionRecord(parent, parent.Scope()).Where(shadow.Exists()).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("correlation accepted shadowing", err)
	}
	var missing *TransactionCorrelatedRecordQuery[outerOwner, innerOwner, transactionTestRecord]
	badLateral := AsTransactionLateral[transactionCorrelationLateral](missing, "bad")
	badJoin := TransactionCrossJoinLateral(parent, badLateral)
	if _, err := SelectTransactionRecord(badJoin, LeftScope(badJoin, parent.Scope())).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil lateral source", err)
	}
}
