package transactionqueries

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type correlatedLateralAlias struct{}
type correlatedGrandchildAlias struct{}

func TestPostgresTransactionLateralRowsAndLocks(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.In(1, 3))), "parent")
	child := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "child")
	c := query.TransactionCorrelate(parent, child)
	p, f := RecordFieldsAt(query.OuterScope(c, parent.Scope())), RecordFieldsAt(query.InnerScope(c, child.Scope()))
	c = c.Where(query.Greater(f.ID, p.ID))
	report := ProjectTransactionCorrelatedSummary(c).SelectID(f.ID.Value()).SelectName(p.Name.Value()).Query().OrderBy(f.ID.Asc()).Limit(1).ForNoKeyUpdate().Of(query.InnerScope(c, child.Scope()))
	lateral := query.AsTransactionLateral[correlatedLateralAlias](report, "chosen")
	left := query.TransactionLeftJoinLateral(parent, lateral)
	rightFields := SummaryNullableFieldsAt(query.NullableRightScope(left, lateral.Scope()))
	parentFields := RecordFieldsAt(query.LeftScope(left, parent.Scope()))
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		rows, err := query.SelectTransactionValue(left, rightFields.ID.Value()).OrderBy(parentFields.ID.Asc()).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			return errors.New("left lateral lost parent rows")
		}
		first, valid := rows[0].Get()
		if !valid || first != 2 || !rows[1].IsNull() {
			return errors.New("left lateral lost match or NULL")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForShare(), 2, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 3, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"inner", "cross"} {
		if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
			var rows []Summary
			var err error
			if kind == "inner" {
				joined := query.TransactionInnerJoinLateral(parent, lateral, query.OnLess(RecordFieldsAt(parent.Scope()).ID, SummaryFieldsAt(lateral.Scope()).ID))
				rows, err = query.SelectTransactionRecord(joined, query.RightScope(joined, lateral.Scope())).All(ctx, tx)
			} else {
				joined := query.TransactionCrossJoinLateral(parent, lateral)
				rows, err = query.SelectTransactionRecord(joined, query.RightScope(joined, lateral.Scope())).All(ctx, tx)
			}
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0] != (Summary{ID: 2, Name: "Alpha"}) {
				return errors.New("lateral generated projection lost parent field or record")
			}
			return nil
		}); err != nil {
			t.Fatal(kind, err)
		}
	}
}

func TestPostgresTransactionLateralSkipLocked(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.Eq(1))), "parent")
	child := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "child")
	c := query.TransactionCorrelate(parent, child)
	p, f := RecordFieldsAt(query.OuterScope(c, parent.Scope())), RecordFieldsAt(query.InnerScope(c, child.Scope()))
	c = c.Where(query.Greater(f.ID, p.ID))
	record := query.SelectTransactionCorrelatedRecord(c, query.InnerScope(c, child.Scope())).OrderBy(f.ID.Asc()).Limit(1).ForUpdate().SkipLocked()
	lateral := query.AsTransactionLateral[correlatedLateralAlias](record, "chosen")
	joined := query.TransactionCrossJoinLateral(parent, lateral)
	if err := inTransaction(ctx, db, path, func(holder *database.Tx) error {
		if _, err := QueryRecords().ForUpdate().RequireFind(ctx, holder, 2); err != nil {
			return err
		}
		return inTransaction(ctx, db, path, func(tx *database.Tx) error {
			row, err := query.SelectTransactionRecord(joined, query.RightScope(joined, lateral.Scope())).RequireFirst(ctx, tx)
			if err != nil {
				return err
			}
			if row.ID != 3 {
				return errors.New("lateral SKIP LOCKED lost available child")
			}
			assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 3, true)
			assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionNestedCorrelatedLocks(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.Eq(1))), "parent")
	child := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "child")
	c := query.TransactionCorrelate(parent, child)
	p, f := RecordFieldsAt(query.OuterScope(c, parent.Scope())), RecordFieldsAt(query.InnerScope(c, child.Scope()))
	grandchild := query.AsTransaction[correlatedGrandchildAlias](query.TransactionOf(QueryRecords()), "grandchild")
	nested := query.TransactionCorrelate(c, grandchild)
	ancestor := RecordFieldsAt(query.OuterScope(nested, query.OuterScope(c, parent.Scope())))
	middle := RecordFieldsAt(query.OuterScope(nested, query.InnerScope(c, child.Scope())))
	grand := RecordFieldsAt(query.InnerScope(nested, grandchild.Scope()))
	nested = nested.Where(query.Greater(grand.ID, middle.ID), query.Greater(grand.ID, ancestor.ID))
	condition := query.SelectTransactionCorrelatedValue(nested, grand.ID.Value()).ForUpdate().Exists()
	selected := query.SelectTransactionCorrelatedValue(c.Where(query.Greater(f.ID, p.ID), condition), f.ID.Value()).ForShare()
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		rows, err := query.SelectTransactionValue(parent, query.TransactionCorrelatedScalarQuery(selected)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return errors.New("nested correlation changed parent count")
		}
		id, valid := rows[0].Get()
		if !valid || id != 2 {
			return errors.New("nested correlation lost grandparent visibility")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForKeyShare(), 3, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionCorrelatedScalarFailuresAndMembership(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.Eq(1))), "parent")
	child := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "child")
	c := query.TransactionCorrelate(parent, child)
	p, f := RecordFieldsAt(query.OuterScope(c, parent.Scope())), RecordFieldsAt(query.InnerScope(c, child.Scope()))
	base := query.SelectTransactionCorrelatedValue(c.Where(query.Greater(f.ID, p.ID)), f.ID.Value())
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		scalar := query.TransactionCorrelatedScalarQuery(base.ForUpdate())
		err := tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := query.SelectTransactionValue(parent, scalar).All(ctx, nested)
			return err
		})
		var failure *database.Error
		if !errors.As(err, &failure) || failure.SQLState() != "21000" {
			return errors.New("correlated scalar hid multiple rows")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, false)
		member := query.TransactionInCorrelatedQuery(query.Add(RecordFieldsAt(parent.Scope()).ID, RecordFieldsAt(parent.Scope()).ID.Param(1)), base.ForShare())
		rows, err := query.SelectTransactionRecord(parent, parent.Scope()).Where(member).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != 1 {
			return errors.New("correlated membership lost parent computation")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 2, true)
		// OF cannot name the enclosing query's source, and a lock cannot aggregate
		// its selected records. Rejection must leave this transaction usable.
		wrongTarget := base.ForUpdate().Of(query.OuterScope(c, parent.Scope()))
		if _, err := query.SelectTransactionRecord(parent, parent.Scope()).Where(wrongTarget.Exists()).All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("correlated lock accepted outer target")
		}
		grouped := query.SelectTransactionCorrelatedValue(c, f.ID.Count().Value()).ForUpdate()
		if _, err := query.SelectTransactionValue(parent, query.TransactionCorrelatedScalarQuery(grouped)).All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("correlated aggregate lock reached SQL")
		}
		_, err = QueryRecords().RequireFind(ctx, tx, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionCorrelationFromNullableLateral(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.In(1, 3))), "parent")
	child := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "child")
	c := query.TransactionCorrelate(parent, child)
	p, f := RecordFieldsAt(query.OuterScope(c, parent.Scope())), RecordFieldsAt(query.InnerScope(c, child.Scope()))
	c = c.Where(query.Equal(f.ID, query.Add(p.ID, p.ID.Param(1))))
	report := ProjectTransactionCorrelatedSummary(c).SelectID(f.ID.Value()).SelectName(f.Name.Value()).Query().ForShare()
	lateral := query.AsTransactionLateral[correlatedLateralAlias](report, "chosen")
	joined := query.TransactionLeftJoinLateral(parent, lateral)
	next := query.AsTransaction[correlatedGrandchildAlias](query.TransactionOf(QueryRecords()), "next_record")
	follow := query.TransactionCorrelate(joined, next)
	previous := SummaryNullableFieldsAt(query.OuterNullableScope(follow, query.NullableRightScope(joined, lateral.Scope())))
	candidate := RecordFieldsAt(query.InnerScope(follow, next.Scope()))
	follow = follow.Where(query.Equal(query.NullableRow(query.Subtract(candidate.ID, candidate.ID.Param(1))), previous.ID))
	scalar := query.TransactionCorrelatedScalarQuery(query.SelectTransactionCorrelatedValue(follow, candidate.ID.Value()).ForKeyShare())
	order := RecordFieldsAt(query.LeftScope(joined, parent.Scope())).ID.Asc()
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		rows, err := query.SelectTransactionValue(joined, scalar).OrderBy(order).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			return errors.New("nullable correlated parent lost rows")
		}
		id, valid := rows[0].Get()
		if !valid || id != 3 || !rows[1].IsNull() {
			return errors.New("nullable parent correlation became a non-null zero value")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 3, false)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 3, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
