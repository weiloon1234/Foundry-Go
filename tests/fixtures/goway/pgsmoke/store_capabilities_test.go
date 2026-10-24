package pgsmoke_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type storeRecord struct {
	ID   model.ID[storeRecord]
	Name string
}
type storeTable struct {
	orm.Table[storeRecord, model.ID[storeRecord]]
	Name orm.Text[storeRecord, string]
}

func TestStoreUUIDAndEachWrites(t *testing.T) {
	db := postgres.Isolate(t).Open(t)
	ctx := t.Context()
	commits := 0
	d := orm.Define("store_records", func(b orm.Builder[storeRecord], r *storeRecord) storeTable {
		return storeTable{orm.TableOf(b, &r.ID), orm.TextOf(b, &r.Name)}
	}, orm.WithHooks(func() orm.Hooks[storeRecord] {
		return orm.Hooks[storeRecord]{AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[storeRecord]) error { commits++; return nil }}
	}))
	_, err := db.Exec(ctx, `CREATE TABLE store_records(id uuid PRIMARY KEY,name text NOT NULL)`)
	must(t, err)
	a, err := model.ParseID[storeRecord]("00000000-0000-0000-0000-000000000001")
	must(t, err)
	b, err := model.ParseID[storeRecord]("00000000-0000-0000-0000-000000000002")
	must(t, err)
	_, err = db.Exec(ctx, `INSERT INTO store_records VALUES($1,'first'),($2,'second')`, a.String(), b.String())
	must(t, err)
	page, err := d.Query().Where(orm.UUIDGreater(d.Key(), a)).OrderBy(d.Key().Asc()).All(ctx, db)
	must(t, err)
	if len(page) != 1 || page[0].ID != b {
		t.Fatal(page)
	}
	sentinel := errors.New("stop")
	_, err = orm.UpdateEach(ctx, db, d.Query(), 2, func(_ context.Context, _ *database.Tx, r storeRecord) ([]orm.Assignment[storeRecord], error) {
		if r.ID == b {
			return nil, sentinel
		}
		return []orm.Assignment[storeRecord]{d.Name.Set("changed")}, nil
	})
	if !errors.Is(err, sentinel) || commits != 0 {
		t.Fatal(err, commits)
	}
	unchanged, err := d.Find(ctx, db, a)
	must(t, err)
	v, ok := unchanged.Get()
	if !ok || v.Name != "first" {
		t.Fatal(unchanged)
	}
	if _, err := orm.DeleteEach(ctx, db, d.Query(), 1); err == nil {
		t.Fatal("overflow accepted")
	}
	got, err := orm.DeleteEach(ctx, db, d.Query(), 2)
	must(t, err)
	if len(got) != 2 || commits != 2 {
		t.Fatal(got, commits)
	}
	count, err := d.Query().Count(ctx, db)
	must(t, err)
	if count != 0 {
		t.Fatal(count)
	}
}

func TestEachWritesWaitForCompleteCandidateSet(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "update"
		if remove {
			name = "delete"
		}
		t.Run(name, func(t *testing.T) {
			db := postgres.Isolate(t).Open(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			callbacks := 0
			d := orm.Define("each_lock_items", func(b orm.Builder[recordItem], r *recordItem) recordTable {
				return recordTable{orm.TableOf(b, &r.ID), orm.TextOf(b, &r.Name)}
			}, orm.WithHooks(func() orm.Hooks[recordItem] {
				return orm.Hooks[recordItem]{
					Updating: func(context.Context, *database.Tx, recordItem, *orm.Write[recordItem]) error { callbacks++; return nil },
					Deleting: func(context.Context, *database.Tx, recordItem) error { callbacks++; return nil },
				}
			}))
			_, err := db.Exec(ctx, `CREATE TABLE each_lock_items(id bigint PRIMARY KEY,name text NOT NULL)`)
			must(t, err)
			_, err = db.Exec(ctx, `INSERT INTO each_lock_items VALUES(1,'first'),(2,'second')`)
			must(t, err)
			type result struct {
				rows []recordItem
				err  error
			}
			done := make(chan result, 1)
			pid := make(chan int64, 1)
			launched, received := false, false
			var batch result
			lockErr := db.Transaction(ctx, func(locker *database.Tx) error {
				if _, err := d.Query().Where(d.Key().Eq(1)).ForUpdate().Require(ctx, locker); err != nil {
					return err
				}
				var blocker int64
				if err := database.ScanOne(ctx, locker, "SELECT pg_backend_pid()", nil, &blocker); err != nil {
					return err
				}
				launched = true
				go func() {
					var rows []recordItem
					err := db.Transaction(ctx, func(writer *database.Tx) error {
						var writerPID int64
						if err := database.ScanOne(ctx, writer, "SELECT pg_backend_pid()", nil, &writerPID); err != nil {
							return err
						}
						pid <- writerPID
						var err error
						if remove {
							rows, err = orm.DeleteEach(ctx, writer, d.Query(), 1)
						} else {
							rows, err = orm.UpdateEach(ctx, writer, d.Query(), 1, func(context.Context, *database.Tx, recordItem) ([]orm.Assignment[recordItem], error) {
								callbacks++
								return []orm.Assignment[recordItem]{d.Name.Set("changed")}, nil
							})
						}
						return err
					})
					done <- result{rows, err}
				}()
				var writerPID int64
				select {
				case writerPID = <-pid:
				case batch = <-done:
					received = true
					return errors.New("batch exited before acquiring candidates")
				case <-ctx.Done():
					return ctx.Err()
				}
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				for {
					var blocked bool
					// Both fixture connections are held by locker and writer. Observe
					// on locker so polling cannot wait for a third pool connection.
					if err := database.ScanOne(ctx, locker, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`, []any{blocker, writerPID}, &blocked); err != nil {
						return err
					}
					// Observe the actual lock wait before releasing the first row. A SKIP
					// LOCKED candidate read would instead finish with only the second row.
					if blocked {
						return nil
					}
					select {
					case batch = <-done:
						received = true
						return errors.New("batch did not wait for the locked matching row")
					case <-ticker.C:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			})
			if lockErr != nil {
				t.Error(lockErr)
			}
			if !launched {
				t.Fatal("writer was not started")
			}
			if !received {
				select {
				case batch = <-done:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if !errors.Is(batch.err, fault.Invalid) || batch.rows != nil || callbacks != 0 {
				t.Errorf("overflow must fail before callbacks: err=%v rows=%v callbacks=%d", batch.err, batch.rows, callbacks)
			}
			unchanged, err := d.Query().OrderBy(d.Key().Asc()).All(ctx, db)
			must(t, err)
			if !reflect.DeepEqual(unchanged, []recordItem{{ID: 1, Name: "first"}, {ID: 2, Name: "second"}}) {
				t.Fatal("partial batch persisted", unchanged)
			}
		})
	}
}
