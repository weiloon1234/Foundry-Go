package transactionqueries

import (
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
	values := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.Select(b.ID)).Where(b.ID.Eq(2))).ForUpdate()
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(rows.TransactionInQuery(expr.Add(a.ID, expr.Select(a.ID).Param(1)), values)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 1 {
			return errors.New("computed membership lost value or scope")
		}
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		p := rows.TransactionExistsQuery(outer, rows.LockedRecord(Records.Query().Where(Records.ID.Eq(2)).ForShare()))
		items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(a.ID.Eq(1), p).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 1 {
			return errors.New("transaction EXISTS lost records")
		}
		assertIndependentLock(t, ctx, db, path, Records.Query().ForNoKeyUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionScalarCardinalityAndNulls(t *testing.T) {
	for _, mode := range []string{"one", "empty", "many", "nullable"} {
		t.Run(mode, func(t *testing.T) {
			ctx, db, path := transactionFixture(t)
			outer := rows.AsTransaction[claimedAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(Records.ID.Eq(1)))), "outer_record")
			inner := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query())), "inner_record")
			fields := recordAt(rows.TransactionScope(inner))
			predicate := fields.ID.Eq(2)
			if mode == "empty" {
				predicate = fields.ID.Eq(0)
			}
			if mode == "many" {
				predicate = expr.OrderRow(fields.ID).Gt(1)
			}
			values := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.Select(fields.ID)).Where(predicate)).ForUpdate()
			scalar := rows.TransactionScalarQuery(outer, values)
			if mode == "nullable" {
				nullable := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.NullFor(fields.ID).Value()).Where(predicate)).ForUpdate()
				scalar = rows.TransactionScalarNullableQuery(outer, nullable)
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
				assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, mode != "empty")
				assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 1, false)
				return nil
			})
			if mode == "many" {
				var failure *database.Error
				if !errors.As(err, &failure) || failure.SQLState() != "21000" {
					t.Fatal("scalar cardinality error", err)
				}
				assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, false)
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
	nullable := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.NullFor(b.ID).Value()).Where(b.ID.Eq(2))).ForShare()
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
			assertIndependentLock(t, ctx, db, path, Records.Query().ForNoKeyUpdate(), 2, true)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		values := rows.TransactionScalarOf(rows.SelectTransactionValue(inner, expr.Select(b.ID)).Where(b.ID.Eq(2))).ForUpdate()
		scalar := rows.TransactionScalarRowQuery(outer, values)
		items, err := rows.SelectTransactionRecord(outer, rows.TransactionScope(outer)).Where(expr.Equal(expr.NullableRow(a.ID), scalar)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != 2 {
			return errors.New("scalar row predicate lost inner SELECT phase")
		}
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
