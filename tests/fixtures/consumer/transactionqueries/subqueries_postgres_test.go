package transactionqueries

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func TestPostgresTransactionMembershipAndExists(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	outer := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords()), "outer_records")
	inner := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "inner_records")
	a, b := RecordFieldsAt(outer.Scope()), RecordFieldsAt(inner.Scope())
	values := query.SelectTransactionValue(inner, b.ID.Value()).Where(b.ID.Eq(2)).ForUpdate()
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		rows, err := query.SelectTransactionRecord(outer, outer.Scope()).Where(query.TransactionInQuery(query.Add(a.ID, a.ID.Param(1)), values)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != 1 {
			return errors.New("computed membership lost value or scope")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		p := query.TransactionExistsQuery(outer, QueryRecords().Where(RecordFields().ID.Eq(2)).ForShare())
		rows, err := query.SelectTransactionRecord(outer, outer.Scope()).Where(a.ID.Eq(1), p).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != 1 {
			return errors.New("transaction EXISTS lost records")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionScalarCardinalityAndNulls(t *testing.T) {
	for _, mode := range []string{"one", "empty", "many", "nullable"} {
		t.Run(mode, func(t *testing.T) {
			ctx, db, path := transactionFixture(t)
			outer := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.Eq(1))), "outer_record")
			inner := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "inner_record")
			fields := RecordFieldsAt(inner.Scope())
			predicate := fields.ID.Eq(2)
			if mode == "empty" {
				predicate = fields.ID.Eq(0)
			}
			if mode == "many" {
				predicate = fields.ID.Gt(1)
			}
			values := query.SelectTransactionValue(inner, fields.ID.Value()).Where(predicate).ForUpdate()
			scalar := query.TransactionScalarQuery(outer, values)
			if mode == "nullable" {
				nullable := query.SelectTransactionValue(inner, query.NullFor(fields.ID).Value()).Where(predicate).ForUpdate()
				scalar = query.TransactionScalarNullableQuery(outer, nullable)
			}
			err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
				rows, err := query.SelectTransactionValue(outer, scalar).All(ctx, tx)
				if err != nil {
					if rows != nil {
						return errors.New("scalar failure retained partial results")
					}
					return err
				}
				if len(rows) != 1 {
					return errors.New("scalar changed outer result count")
				}
				id, valid := rows[0].Get()
				if mode == "one" && (!valid || id != 2) {
					return errors.New("scalar lost concrete result")
				}
				if (mode == "nullable" || mode == "empty") && valid {
					return errors.New("scalar lost SQL NULL")
				}
				assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, mode != "empty")
				assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
				return nil
			})
			if mode == "many" {
				var failure *database.Error
				if !errors.As(err, &failure) || failure.SQLState() != "21000" {
					t.Fatal("scalar cardinality error", err)
				}
				assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, false)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresTransactionNullableMembershipAndScalarRow(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	outer := query.AsTransaction[claimedAlias](query.TransactionOf(QueryRecords()), "outer_records")
	inner := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords()), "inner_records")
	a, b := RecordFieldsAt(outer.Scope()), RecordFieldsAt(inner.Scope())
	nullable := query.SelectTransactionValue(inner, query.NullFor(b.ID).Value()).Where(b.ID.Eq(2)).ForShare()
	predicate := query.TransactionInNullableQuery(a.ID, nullable)
	for _, p := range []query.Predicate[query.TransactionAlias[claimedAlias, Record]]{predicate, predicate.Not()} {
		if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
			rows, err := query.SelectTransactionRecord(outer, outer.Scope()).Where(p).All(ctx, tx)
			if err != nil {
				return err
			}
			if len(rows) != 0 {
				return errors.New("NULL membership stopped using three-valued logic")
			}
			assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 2, true)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		values := query.SelectTransactionValue(inner, b.ID.Value()).Where(b.ID.Eq(2)).ForUpdate()
		scalar := query.TransactionScalarRowQuery(outer, values)
		rows, err := query.SelectTransactionRecord(outer, outer.Scope()).Where(query.Equal(query.NullableRow(a.ID), scalar)).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != 2 {
			return errors.New("scalar row predicate lost inner SELECT phase")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
