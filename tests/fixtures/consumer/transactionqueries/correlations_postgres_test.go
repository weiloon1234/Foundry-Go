package transactionqueries

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type correlatedLateralAlias struct{}
type correlatedGrandchildAlias struct{}

func TestPostgresTransactionLateralRowsAndLocks(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyIn(Records.ID, 1, 3)))), "parent")
	child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
	c = c.Where(expr.Greater(f.ID, p.ID))
	report := rows.ProjectTransactionCorrelated(c, Summaries, rows.MapSelected(Summaries.ID, expr.AsSelected(expr.Row(f.ID))), rows.MapSelected(Summaries.Name, expr.AsSelected(expr.Row(p.Name)))).OrderBy(expr.Asc(f.ID)).Limit(1).ForNoKeyUpdate().Of(rows.Target(rows.TransactionInnerScope(c, rows.TransactionScope(child))))
	lateral := rows.AsTransactionLateral[correlatedLateralAlias](report, "chosen")
	left := rows.TransactionLeftJoinLateral(parent, lateral)
	rightFields := nullableSummaryAt(rows.NullableRightScope(left, rows.TransactionLateralScope(lateral)))
	parentFields := recordAt(rows.LeftScope(left, rows.TransactionScope(parent)))
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		items, err := rows.SelectTransactionValue(left, expr.AsSelected(expr.Row(rightFields.ID))).OrderBy(expr.Asc(parentFields.ID)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			return errors.New("left lateral lost parent rows")
		}
		first, valid := items[0].Get()
		if !valid || first != 2 || !items[1].IsNull() {
			return errors.New("left lateral lost match or NULL")
		}
		assertIndependentLock(t, ctx, db, path, orm.ForShare(Records.Query()), 2, true)
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 1, false)
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 3, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"inner", "cross"} {
		if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
			var items []Summary
			var err error
			if kind == "inner" {
				joined := rows.TransactionInnerJoinLateral(parent, lateral, rows.OnLess(recordAt(rows.TransactionScope(parent)).ID, rows.At(rows.TransactionLateralScope(lateral), Summaries.ID)))
				items, err = rows.SelectTransactionRecord(joined, rows.RightScope(joined, rows.TransactionLateralScope(lateral))).All(ctx, tx)
			} else {
				joined := rows.TransactionCrossJoinLateral(parent, lateral)
				items, err = rows.SelectTransactionRecord(joined, rows.RightScope(joined, rows.TransactionLateralScope(lateral))).All(ctx, tx)
			}
			if err != nil {
				return err
			}
			if len(items) != 1 || items[0] != (Summary{ID: 2, Name: "Alpha"}) {
				return errors.New("lateral projection lost parent field or record")
			}
			return nil
		}); err != nil {
			t.Fatal(kind, err)
		}
	}
}

func TestPostgresTransactionLateralSkipLocked(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyEq(Records.ID, 1)))), "parent")
	child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
	c = c.Where(expr.Greater(f.ID, p.ID))
	record := rows.SelectTransactionCorrelatedRecord(c, rows.TransactionInnerScope(c, rows.TransactionScope(child))).OrderBy(expr.Asc(f.ID)).Limit(1).ForUpdate().SkipLocked()
	lateral := rows.AsTransactionLateral[correlatedLateralAlias](record, "chosen")
	joined := rows.TransactionCrossJoinLateral(parent, lateral)
	if err := inTransaction(ctx, db, path, func(holder *database.Tx) error {
		if _, err := orm.RequireLocked(ctx, holder, orm.Lock(Records.Query()).Where(orm.KeyEq(Records.ID, 2))); err != nil {
			return err
		}
		return inTransaction(ctx, db, path, func(tx *database.Tx) error {
			row, err := rows.SelectTransactionRecord(joined, rows.RightScope(joined, rows.TransactionLateralScope(lateral))).RequireFirst(ctx, tx)
			if err != nil {
				return err
			}
			if row.ID != 3 {
				return errors.New("lateral SKIP LOCKED lost available child")
			}
			assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 3, true)
			assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 1, false)
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionNestedCorrelatedLocks(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyEq(Records.ID, 1)))), "parent")
	child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
	grandchild := rows.AsTransaction[correlatedGrandchildAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "grandchild")
	nested := rows.TransactionCorrelate(c, grandchild)
	ancestor := recordAt(rows.TransactionOuterScope(nested, rows.TransactionOuterScope(c, rows.TransactionScope(parent))))
	middle := recordAt(rows.TransactionOuterScope(nested, rows.TransactionInnerScope(c, rows.TransactionScope(child))))
	grand := recordAt(rows.TransactionInnerScope(nested, rows.TransactionScope(grandchild)))
	nested = nested.Where(expr.Greater(grand.ID, middle.ID), expr.Greater(grand.ID, ancestor.ID))
	condition := rows.SelectTransactionCorrelatedScalar(nested, expr.AsSelected(expr.Row(grand.ID))).ForUpdate().Exists()
	selected := rows.SelectTransactionCorrelatedScalar(c.Where(expr.Greater(f.ID, p.ID), condition), expr.AsSelected(expr.Row(f.ID))).ForShare()
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		items, err := rows.SelectTransactionValue(parent, expr.AsSelected(rows.TransactionCorrelatedScalarRowQuery(selected))).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 {
			return errors.New("nested correlation changed parent count")
		}
		id, valid := items[0].Get()
		if !valid || id != 2 {
			return errors.New("nested correlation lost grandparent visibility")
		}
		assertIndependentLock(t, ctx, db, path, orm.ForNoKeyUpdate(Records.Query()), 2, true)
		assertIndependentLock(t, ctx, db, path, orm.ForKeyShare(Records.Query()), 3, true)
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionCorrelatedScalarFailuresAndMembership(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyEq(Records.ID, 1)))), "parent")
	child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
	base := rows.SelectTransactionCorrelatedScalar(c.Where(expr.Greater(f.ID, p.ID)), expr.AsSelected(expr.Row(f.ID)))
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		scalar := expr.AsSelected(rows.TransactionCorrelatedScalarRowQuery(base.ForUpdate()))
		err := tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := rows.SelectTransactionValue(parent, scalar).All(ctx, nested)
			return err
		})
		var failure *database.Error
		if !errors.As(err, &failure) || failure.SQLState() != "21000" {
			return errors.New("correlated scalar hid multiple items")
		}
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 2, false)
		member := rows.TransactionInCorrelatedQuery(expr.Add(recordAt(rows.TransactionScope(parent)).ID, expr.ParamOf(expr.AsSelected(expr.Row(recordAt(rows.TransactionScope(parent)).ID)), 1)), base.ForShare())
		items, err := rows.SelectTransactionRecord(parent, rows.TransactionScope(parent)).Where(member).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 1 {
			return errors.New("correlated membership lost parent computation")
		}
		assertIndependentLock(t, ctx, db, path, orm.ForNoKeyUpdate(Records.Query()), 2, true)
		// OF cannot name the enclosing query's source, and a lock cannot aggregate
		// its selected records. Rejection must leave this transaction usable.
		wrongTarget := base.ForUpdate().Of(rows.Target(rows.TransactionOuterScope(c, rows.TransactionScope(parent))))
		if _, err := rows.SelectTransactionRecord(parent, rows.TransactionScope(parent)).Where(wrongTarget.Exists()).All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("correlated lock accepted outer target")
		}
		grouped := rows.SelectTransactionCorrelatedScalar(c, expr.AsSelected(expr.CountFieldAt(f.scope, orm.KeyExpr(Records.ID)))).ForUpdate()
		if _, err := rows.SelectTransactionValue(parent, expr.AsSelected(rows.TransactionCorrelatedScalarRowQuery(grouped))).All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("correlated aggregate lock reached SQL")
		}
		_, err = orm.Require(ctx, tx, Records.Query().Where(orm.KeyEq(Records.ID, 1)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionCorrelationFromNullableLateral(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	parent := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyIn(Records.ID, 1, 3)))), "parent")
	child := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "child")
	c := rows.TransactionCorrelate(parent, child)
	p, f := recordAt(rows.TransactionOuterScope(c, rows.TransactionScope(parent))), recordAt(rows.TransactionInnerScope(c, rows.TransactionScope(child)))
	c = c.Where(expr.Equal(f.ID, expr.Add(p.ID, expr.ParamOf(expr.AsSelected(expr.Row(p.ID)), 1))))
	report := rows.ProjectTransactionCorrelated(c, Summaries, rows.MapSelected(Summaries.ID, expr.AsSelected(expr.Row(f.ID))), rows.MapSelected(Summaries.Name, expr.AsSelected(expr.Row(f.Name)))).ForShare()
	lateral := rows.AsTransactionLateral[correlatedLateralAlias](report, "chosen")
	joined := rows.TransactionLeftJoinLateral(parent, lateral)
	next := rows.AsTransaction[correlatedGrandchildAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "next_record")
	follow := rows.TransactionCorrelate(joined, next)
	previous := nullableSummaryAt(rows.TransactionOuterNullableScope(follow, rows.NullableRightScope(joined, rows.TransactionLateralScope(lateral))))
	candidate := recordAt(rows.TransactionInnerScope(follow, rows.TransactionScope(next)))
	follow = follow.Where(expr.Equal(expr.NullableRow(expr.Subtract(candidate.ID, expr.ParamOf(expr.AsSelected(expr.Row(candidate.ID)), 1))), previous.ID))
	scalar := expr.AsSelected(rows.TransactionCorrelatedScalarRowQuery(rows.SelectTransactionCorrelatedScalar(follow, expr.AsSelected(expr.Row(candidate.ID))).ForKeyShare()))
	order := expr.Asc(recordAt(rows.LeftScope(joined, rows.TransactionScope(parent))).ID)
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		items, err := rows.SelectTransactionValue(joined, scalar).OrderBy(order).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			return errors.New("nullable correlated parent lost items")
		}
		id, valid := items[0].Get()
		if !valid || id != 3 || !items[1].IsNull() {
			return errors.New("nullable parent correlation became a non-null zero value")
		}
		assertIndependentLock(t, ctx, db, path, orm.ForNoKeyUpdate(Records.Query()), 2, true)
		assertIndependentLock(t, ctx, db, path, orm.ForNoKeyUpdate(Records.Query()), 3, false)
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 3, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
