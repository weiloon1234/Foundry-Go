package transactionqueries

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
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
		_, err := QueryRecords().CreateMany(ctx, tx, []RecordDraft{
			RecordDraft{}.SetID(1).SetName("Alpha"),
			RecordDraft{}.SetID(2).SetName("Beta"),
			RecordDraft{}.SetID(3).SetName("Gamma"),
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
func assertIndependentLock(t *testing.T, ctx context.Context, db *database.DB, path string, q RecordLockedQuery, id int64, blocked bool) {
	t.Helper()
	err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := q.NoWait().RequireFind(ctx, tx, id)
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

func recordCTE(q query.TransactionRecordSource[Record]) query.TransactionQuery[query.TransactionAlias[claimedAlias, Record], Record] {
	source := query.AsTransaction[claimedAlias](query.TransactionCTE("claimed", q), "candidate")
	return query.SelectTransactionRecord(source, source.Scope())
}

func TestPostgresTransactionCTESkipLockedAndProjection(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	err := inTransaction(ctx, db, path, func(holder *database.Tx) error {
		if _, err := QueryRecords().ForUpdate().RequireFind(ctx, holder, 1); err != nil {
			return err
		}
		return inTransaction(ctx, db, path, func(claimant *database.Tx) error {
			rows, err := Claimed(ctx, claimant)
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0] != (Summary{ID: 2, Name: "Beta"}) {
				return errors.New("locked CTE lost its limit, skipped row or generated projection")
			}
			assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, true)
			assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 3, false)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, false)
}

func TestPostgresTransactionCTEAggregatesAndExistence(t *testing.T) {
	for _, mode := range []string{"count", "projection", "exists"} {
		t.Run(mode, func(t *testing.T) {
			ctx, db, path := transactionFixture(t)
			if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
				q := QueryRecords().OrderBy(RecordFields().ID.Asc()).ForUpdate()
				switch mode {
				case "count":
					count, err := recordCTE(q).Count(ctx, tx)
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
						return errors.New("outer aggregate lost complete generated result")
					}
				case "exists":
					exists, err := recordCTE(q.Limit(1)).Exists(ctx, tx)
					if err != nil {
						return err
					}
					if !exists {
						return errors.New("CTE existence discarded selected row")
					}
				}
				assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, true)
				assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, mode != "exists")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresTransactionDerivedAndNestedJoinLocks(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	first := QueryRecords().Where(RecordFields().ID.Eq(1)).ForNoKeyUpdate()
	inner := query.AsTransaction[claimedAlias](query.TransactionCTE("first_claim", first).Materialized(), "inner_claim")
	nested := query.TransactionCTE("second_claim", query.SelectTransactionRecord(inner, inner.Scope())).NotMaterialized()
	left := query.AsTransaction[claimedAlias](nested, "candidate")
	right := query.AsTransaction[otherAlias](query.TransactionOf(QueryRecords().Where(RecordFields().ID.Eq(2))), "other")
	a, b := RecordFieldsAt(left.Scope()), RecordFieldsAt(right.Scope())
	joined := query.TransactionInnerJoin(left, right, query.OnEqual(query.Add(a.ID, a.ID.Param(1)), b.ID))
	leftScope, rightScope := query.LeftScope(joined, left.Scope()), query.RightScope(joined, right.Scope())
	q := query.SelectTransactionRecord(joined, leftScope).ForKeyShare().Of(rightScope)
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		row, err := q.RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		if row != (Record{ID: 1, Name: "Alpha"}) {
			return errors.New("nested CTE/join lost complete record")
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForShare(), 1, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForNoKeyUpdate(), 2, false)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 3, false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	derived := query.AsTransaction[claimedAlias](first, "derived")
	if err := inTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := query.SelectTransactionRecord(derived, derived.Scope()).RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForShare(), 1, true)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, false)
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
			if _, err := recordCTE(QueryRecords().Where(RecordFields().ID.Eq(1)).ForUpdate()).All(ctx, nested); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			return errors.New("savepoint lost rollback error")
		}
		if err := tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := recordCTE(QueryRecords().Where(RecordFields().ID.Eq(2)).ForUpdate()).All(ctx, nested)
			return err
		}); err != nil {
			return err
		}
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 1, false)
		assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertIndependentLock(t, ctx, db, path, QueryRecords().ForUpdate(), 2, false)
}

func TestPostgresTransactionComposedCancellationAndValidation(t *testing.T) {
	ctx, db, path := transactionFixture(t)
	q := recordCTE(QueryRecords().Where(RecordFields().ID.Eq(1)).ForUpdate())
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
		if _, err := q.ForUpdate().All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("outer CTE lock target was not rejected before SQL")
		}
		distinct := recordCTE(QueryRecords().Distinct().ForUpdate())
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
