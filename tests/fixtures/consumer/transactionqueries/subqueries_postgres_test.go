package transactionqueries

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"github.com/weiloon1234/Foundry-Go/value"

	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestPostgresTransactionMembershipAndExists(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	outer := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "outer_records")
	inner := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "inner_records")
	a, b := recordAt(rows.TransactionScope(outer)), recordAt(rows.TransactionScope(inner))
	values := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.AsSelected(expr.Row(b.ID))).Where(expr.Eq(b.ID, 2))).ForUpdate()
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(rows.TransactionInQuery(expr.Add(a.ID, expr.ParamOf(expr.AsSelected(expr.Row(a.ID)), 1)), values)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 1 {
			return errors.New("computed membership lost value or scope")
		}
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 2, true)
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		p := rows.TransactionExistsQuery(outer, rows.LockedRecord(orm.ForShare(Records.Query().Where(orm.KeyEq(Records.ID, 2)))))
		items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(expr.Eq(a.ID, 1), p).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 1 {
			return errors.New("transaction EXISTS lost records")
		}
		assertIndependentLock(t, ctx, db, path, orm.ForNoKeyUpdate(Records.Query()), 2, true)
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionScalarCardinalityAndNulls(t *testing.T) {
	for _, mode := range []string{"one", "empty", "many", "nullable"} {
		t.Run(mode, func(t *testing.T) {
			ctx, db, path := transactionFixture(t)
			outer := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(orm.KeyEq(Records.ID, 1)))), "outer_record")
			inner := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "inner_record")
			fields := recordAt(rows.TransactionScope(inner))
			predicate := expr.Eq(fields.ID, 2)
			if mode == "empty" {
				predicate = expr.Eq(fields.ID, 0)
			}
			if mode == "many" {
				predicate = expr.Gt(expr.OrderRow(fields.ID), 1)
			}
			values := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.AsSelected(expr.Row(fields.ID))).Where(predicate)).ForUpdate()
			scalar := expr.AsSelected(rows.TransactionScalarRowQuery(outer, values))
			if mode == "nullable" {
				nullable := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.AsSelected(expr.ParamOf(expr.NullableRow(fields.ID), value.Null[int64]()))).Where(predicate)).ForUpdate()
				scalar = expr.AsSelected(rows.TransactionScalarNullableRowQuery(outer, nullable))
			}
			err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
				items, err := rows.SelectTransactionValue(outer, scalar).All(ctx, tx)
				if err != nil {
					if items != nil {
						return errors.New("scalar failure retained partial results")
					}
					return err
				}
				if len(items) != 1 {
					return errors.New("scalar changed outer result count")
				}
				id, valid := items[0].Get()
				if mode == "one" && (!valid || id != 2) {
					return errors.New("scalar lost concrete result")
				}
				if (mode == "nullable" || mode == "empty") && valid {
					return errors.New("scalar lost SQL NULL")
				}
				assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 2, mode != "empty")
				assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 1, false)
				return nil
			})
			if mode == "many" {
				var failure *database.Error
				if !errors.As(err, &failure) || failure.SQLState() != "21000" {
					t.Fatal("scalar cardinality error", err)
				}
				assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 2, false)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresTransactionNullableMembershipAndScalarRow(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	outer := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "outer_records")
	inner := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "inner_records")
	a, b := recordAt(rows.TransactionScope(outer)), recordAt(rows.TransactionScope(inner))
	nullable := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.AsSelected(expr.ParamOf(expr.NullableRow(b.ID), value.Null[int64]()))).Where(expr.Eq(b.ID, 2))).ForShare()
	predicate := rows.TransactionInNullableQuery(a.ID, nullable)
	for _, p := range []expr.Predicate[rows.TransactionAlias[claimedAlias, Record]]{predicate, expr.Not(predicate)} {
		if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
			items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(p).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(items) != 0 {
				return errors.New("NULL membership stopped using three-valued logic")
			}
			assertIndependentLock(t, ctx, db, path, orm.ForNoKeyUpdate(Records.Query()), 2, true)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		values := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.AsSelected(expr.Row(b.ID))).Where(expr.Eq(b.ID, 2))).ForUpdate()
		scalar := rows.TransactionScalarRowQuery(outer, values)
		items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(expr.Equal(expr.NullableRow(a.ID), scalar)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 2 {
			return errors.New("scalar row predicate lost inner SELECT phase")
		}
		assertIndependentLock(t, ctx, db, path, orm.Lock(Records.Query()), 2, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
