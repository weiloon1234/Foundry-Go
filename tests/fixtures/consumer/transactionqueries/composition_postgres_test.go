package transactionqueries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func transactionFixture(t *testing.T) (context.Context, *database.DB, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	db := pgtest.Open(t)
	path := `SET LOCAL search_path TO "` + pgtest.Namespace(t, db) + `"`
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE records (id bigint PRIMARY KEY,name text NOT NULL)`); err != nil {
			return err
		}
		_, err := orm.InsertEach(ctx, tx, Records.Table, [][]orm.Assignment[Record]{
			{Records.ID.Set(1), Records.Name.Set("Alpha")},
			{Records.ID.Set(2), Records.Name.Set("Beta")},
			{Records.ID.Set(3), Records.Name.Set("Gamma")},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ctx, db, path
}

func inTransaction(ctx context.Context, db *database.DB, path string, fn func(*database.Tx) error) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Each call starts an independent transaction, even when a surrounding callback
// already holds a transaction. NOWAIT makes lock ownership observable without sleeps.
func assertIndependentLock(t *testing.T, ctx context.Context, db *database.DB, path string, q orm.LockedQuery[Record], id int64, blocked bool) {
	t.Helper()
	err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := q.NoWait().Where(Records.ID.Eq(id)).Require(ctx, tx)
		return err
	})
	if !blocked {
		if err != nil {
			t.Fatalf("record %d should be available: %v", id, err)
		}
		return
	}
	var failure *database.Error
	if !errors.As(err, &failure) || failure.SQLState() != "55P03" {
		t.Fatalf("record %d should be locked: %v", id, err)
	}
}

func recordCTE(q rows.TransactionRecordSource[Record]) rows.TransactionRead[rows.TransactionAlias[claimedAlias, Record], Record] {
	source := rows.AsTransaction[claimedAlias](rows.TransactionCTE("claimed", q), "candidate")
	return rows.SelectTransactionRecord(source, rows.TransactionScope(source))
}

func TestPostgresTransactionCTESkipLockedAndProjection(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	err := inTransaction(ctx, db, path, func(holder *database.Tx) error {
		if _, err := Records.Query().ForUpdate().Where(Records.ID.Eq(1)).Require(ctx, holder); err != nil {
			return err
		}
		return inTransaction(ctx, db, path, func(claimant *database.Tx) error {
			items, err := Claimed(ctx, claimant)
			if err != nil {
				return err
			}
			if len(items) != 1 || items[0] != (Summary{ID: 2, Name: "Beta"}) {
				return errors.New("locked CTE lost its limit, skipped row or projection")
			}
			assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, true)
			assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 3, false)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, false)
}

func TestPostgresTransactionCTEAggregatesAndExistence(t *testing.T) {
	for _, mode := range []string{"count", "projection", "exists"} {
		t.Run(mode, func(t *testing.T) {
			ctx, db, path := transactionFixture(t)
			if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
				q := Records.Query().OrderBy(Records.ID.Asc()).ForUpdate()
				switch mode {
				case "count":
					count, err := recordCTE(rows.LockedRecord(q)).Count(ctx, tx)
					if err != nil {
						return err
					}
					if count != 3 {
						return errors.New("CTE count changed selected records")
					}
				case "projection":
					result, err := CountClaimed(ctx, tx)
					if err != nil {
						return err
					}
					if result.Count != 3 {
						return errors.New("outer aggregate lost complete result")
					}
				case "exists":
					exists, err := recordCTE(rows.LockedRecord(q.Limit(1))).Exists(ctx, tx)
					if err != nil {
						return err
					}
					if !exists {
						return errors.New("CTE existence discarded selected row")
					}
				}
				assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 1, true)
				assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, mode != "exists")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresTransactionDerivedAndNestedJoinLocks(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	first := Records.Query().Where(Records.ID.Eq(1)).ForNoKeyUpdate()
	inner := rows.AsTransaction[claimedAlias](rows.TransactionCTE("first_claim", rows.LockedRecord(first)).Materialized(), "inner_claim")
	nested := rows.TransactionCTE("second_claim", rows.SelectTransactionRecord(inner, rows.TransactionScope(inner))).NotMaterialized()
	left := rows.AsTransaction[claimedAlias](nested, "candidate")
	right := rows.AsTransaction[otherAlias](rows.TransactionOf(rows.ModelRecord(Records.Query().Where(Records.ID.Eq(2)))), "other")
	a, b := recordAt(rows.TransactionScope(left)), recordAt(rows.TransactionScope(right))
	joined := rows.TransactionInnerJoin(left, right, rows.OnEqual(expr.Add(a.ID, expr.Row(a.ID).Value().Param(1)), b.ID))
	leftScope, rightScope := rows.LeftScope(joined, rows.TransactionScope(left)), rows.RightScope(joined, rows.TransactionScope(right))
	q := rows.ForKeyShare(rows.SelectTransactionRecord(joined, leftScope)).Of(rows.Target(rightScope))
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		row, err := q.RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		if row != (Record{ID: 1, Name: "Alpha"}) {
			return errors.New("nested CTE/join lost complete record")
		}
		assertIndependentLock(t, ctx, db, path, Records.Query().ForShare(), 1, true)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForNoKeyUpdate(), 2, false)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 3, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	derived := rows.AsTransaction[claimedAlias](rows.LockedRecord(first), "derived")
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := rows.SelectTransactionRecord(derived, rows.TransactionScope(derived)).RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		assertIndependentLock(t, ctx, db, path, Records.Query().ForShare(), 1, true)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTransactionComposedSavepoints(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	rollback := errors.New("roll back composed lock")
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		err := tx.Transaction(ctx, func(nested *database.Tx) error {
			if _, err := recordCTE(rows.LockedRecord(Records.Query().Where(Records.ID.Eq(1)).ForUpdate())).All(ctx, nested); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			return errors.New("savepoint lost rollback error")
		}
		if err := tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := recordCTE(rows.LockedRecord(Records.Query().Where(Records.ID.Eq(2)).ForUpdate())).All(ctx, nested)
			return err
		}); err != nil {
			return err
		}
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 1, false)
		assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertIndependentLock(t, ctx, db, path, Records.Query().ForUpdate(), 2, false)
}

func TestPostgresTransactionComposedCancellationAndValidation(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	q := recordCTE(rows.LockedRecord(Records.Query().Where(Records.ID.Eq(1)).ForUpdate()))
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		if _, err := q.All(ctx, tx); err != nil {
			return err
		}
		err := inTransaction(ctx, db, path, func(waiter *database.Tx) error {
			short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			_, err := q.All(short, waiter)
			return err
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			return errors.New("composed lock wait lost cancellation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		if _, err := rows.ForUpdate(q).All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("outer CTE lock target was not rejected before SQL")
		}
		distinct := recordCTE(rows.ForUpdate(rows.Read(rows.Distinct(rows.FromModel(Records.Query())))))
		if _, err := distinct.All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("nested distinct lock was not rejected before SQL")
		}
		row, err := q.RequireFirst(ctx, tx)
		if err == nil && row.ID != 1 {
			return errors.New("validation failure changed transaction result")
		}
		return err
	}); err != nil {
		t.Fatal("invalid composed query poisoned transaction", err)
	}
}
