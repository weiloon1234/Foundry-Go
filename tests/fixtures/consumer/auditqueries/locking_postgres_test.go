package auditqueries_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/auditqueries"
	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
)

func auditBackendPID(ctx context.Context, tx *database.Tx) (int, error) {
	rows, err := tx.Query(ctx, `SELECT pg_backend_pid()`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, errors.Join(errors.New("missing backend pid"), rows.Err())
	}
	var pid int
	if err := rows.Scan(&pid); err != nil {
		return 0, err
	}
	return pid, rows.Close()
}

// Observe PostgreSQL's blocker graph, not a goroutine scheduling delay.
func waitAuditBlocker(ctx context.Context, c consumer, blocker, waiter int, done <-chan struct{}) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := c.within(ctx, func(tx *database.Tx) error {
			rows, err := tx.Query(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, blocker, waiter)
			if err != nil {
				return err
			}
			defer rows.Close()
			if !rows.Next() {
				return errors.Join(errors.New("missing blocker state"), rows.Err())
			}
			if err := rows.Scan(&blocked); err != nil {
				return err
			}
			return rows.Close()
		}); err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-done:
			return errors.New("writer finished before the expected lock wait")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Readiness means the operation finished and its outer transaction still owns
// the lock. Cleanup releases and joins the holder even when an assertion fails.
func holdAuditTransaction(t *testing.T, ctx context.Context, c consumer, operation func(*database.Tx) error) (int, func()) {
	t.Helper()
	ready, stop, done := make(chan int, 1), make(chan struct{}), make(chan struct{})
	var result error
	var once sync.Once
	finish := func() {
		once.Do(func() {
			close(stop)
			<-done
			if result != nil {
				t.Error("audit lock holder", result)
			}
		})
	}
	t.Cleanup(finish)
	go func() {
		defer close(done)
		result = c.within(ctx, func(tx *database.Tx) error {
			pid, err := auditBackendPID(ctx, tx)
			if err != nil {
				return err
			}
			if err := operation(tx); err != nil {
				return err
			}
			ready <- pid
			select {
			case <-stop:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case pid := <-ready:
		return pid, finish
	case <-done:
		t.Fatal("audit lock holder failed before readiness", result)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return 0, finish
}

func TestAuditedBeforeImageLocks(t *testing.T) {
	for _, each := range []bool{false, true} {
		for _, operation := range []lifecycle.Operation{lifecycle.Update, lifecycle.SoftDelete, lifecycle.Restore, lifecycle.Delete, lifecycle.ForceDelete} {
			name := operation.String()
			if each {
				name += "Each"
			}
			t.Run(name, func(t *testing.T) {
				c := prepare(t, true)
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				f := auditqueries.Accounts
				input := draft(t, "locking@example.test")
				var account auditqueries.Account
				var label auditqueries.Label
				if err := c.within(ctx, func(tx *database.Tx) error {
					var err error
					account, err = orm.Insert(ctx, tx, f.Table, input...)
					if err != nil {
						return err
					}
					if operation == lifecycle.Restore {
						if _, err := orm.Delete(ctx, tx, f.Query(), f.Key(), account.ID); err != nil {
							return err
						}
					}
					label, err = orm.Insert(ctx, tx, auditqueries.Labels.Table, orm.KeySet(auditqueries.Labels.Key(), "locked"), orm.TextSet(auditqueries.Labels.Name, "original"))
					return err
				}); err != nil {
					t.Fatal(err)
				}
				_, release := holdAuditTransaction(t, ctx, c, func(tx *database.Tx) error {
					if operation == lifecycle.Delete {
						_, err := orm.RequireLocked(ctx, tx, orm.ForKeyShare(auditqueries.Labels.Query().Where(orm.KeyEq(auditqueries.Labels.Key(), label.Code))))
						return err
					}
					_, err := orm.RequireLocked(ctx, tx, orm.ForKeyShare(f.Query().WithTrashed().Where(orm.KeyEq(f.Key(), account.ID))))
					return err
				})
				defer release()
				write := func(tx *database.Tx) error {
					var err error
					if each {
						switch operation {
						case lifecycle.Update:
							_, err = orm.UpdateEach(ctx, tx, f.Query().Where(orm.KeyEq(f.Key(), account.ID)), 1, func(context.Context, *database.Tx, auditqueries.Account) ([]orm.Assignment[auditqueries.Account], error) {
								return []orm.Assignment[auditqueries.Account]{orm.TextSet(f.PasswordHash, "changed")}, nil
							})
						case lifecycle.SoftDelete:
							_, err = orm.DeleteEach(ctx, tx, f.Query().Where(orm.KeyEq(f.Key(), account.ID)), 1)
						case lifecycle.Restore:
							_, err = orm.RestoreEach(ctx, tx, f.Query().OnlyTrashed().Where(orm.KeyEq(f.Key(), account.ID)), f.Soft, 1)
						case lifecycle.Delete:
							_, err = orm.DeleteEach(ctx, tx, auditqueries.Labels.Query().Where(orm.KeyEq(auditqueries.Labels.Key(), label.Code)), 1)
						case lifecycle.ForceDelete:
							_, err = orm.ForceDeleteEach(ctx, tx, f.Query().Where(orm.KeyEq(f.Key(), account.ID)), f.Soft, 1)
						}
						return err
					}
					switch operation {
					case lifecycle.Update:
						_, err = orm.Update(ctx, tx, f.Query(), f.Key(), account.ID, orm.TextSet(f.PasswordHash, "changed"))
					case lifecycle.SoftDelete:
						_, err = orm.Delete(ctx, tx, f.Query(), f.Key(), account.ID)
					case lifecycle.Restore:
						_, err = orm.Restore(ctx, tx, f.Table, account.ID)
					case lifecycle.Delete:
						_, err = orm.Delete(ctx, tx, auditqueries.Labels.Query(), auditqueries.Labels.Key(), label.Code)
					case lifecycle.ForceDelete:
						_, err = orm.ForceDelete(ctx, tx, f.Query(), f.Soft, f.Key(), account.ID)
					}
					return err
				}
				err := c.within(ctx, func(tx *database.Tx) error {
					if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
						return err
					}
					return write(tx)
				})
				if operation == lifecycle.Delete || operation == lifecycle.ForceDelete {
					var failure *database.Error
					if !errors.As(err, &failure) || failure.SQLState() != "55P03" {
						t.Fatal("physical deletion did not block on KEY SHARE", err)
					}
					release()
					if err := c.within(ctx, write); err != nil {
						t.Fatal("delete after KEY SHARE release", err)
					}
				} else if err != nil {
					t.Fatal("audited SQL update blocked on KEY SHARE", err)
				}
				if err := c.within(ctx, func(tx *database.Tx) error {
					if operation == lifecycle.Delete {
						page, err := audit.ModelHistory(ctx, tx, c.recorder, auditqueries.LabelIdentity.ReferenceOf(label), orm.PageRequest{Number: 1, Size: 10})
						if err != nil {
							return err
						}
						if page.Total != 2 {
							return errors.New("physical delete did not record exactly once")
						}
						_, err = orm.RequireKey(ctx, tx, auditqueries.Labels.Table, label.Code)
						if !errors.Is(err, database.NotFound) {
							return errors.New("physical delete retained its row")
						}
						return nil
					}
					page := history(t, c, ctx, tx, account)
					want := int64(2)
					if operation == lifecycle.Restore {
						want = 3
					}
					if page.Total != want || page.Items[0].Changes().Operation() != operation {
						return errors.New("audited operation missing or duplicated")
					}
					stored, err := orm.Require(ctx, tx, f.Query().WithTrashed().Where(orm.KeyEq(f.Key(), account.ID)))
					if operation == lifecycle.ForceDelete {
						if !errors.Is(err, database.NotFound) {
							return errors.New("force delete retained its row")
						}
						return nil
					}
					if err != nil {
						return err
					}
					if operation == lifecycle.Update && stored.PasswordHash != "changed" ||
						operation == lifecycle.SoftDelete && stored.DeletedAt.IsNull() ||
						operation == lifecycle.Restore && !stored.DeletedAt.IsNull() {
						return errors.New("audited write did not persist")
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		}

	}
	t.Run("serialized-before-images", func(t *testing.T) {
		c := prepare(t, true)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		f := auditqueries.Labels
		var label auditqueries.Label
		if err := c.within(ctx, func(tx *database.Tx) error {
			var err error
			label, err = orm.Insert(ctx, tx, f.Table, orm.KeySet(f.Key(), "serialized"), orm.TextSet(f.Name, "original"))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		blocker, release := holdAuditTransaction(t, ctx, c, func(tx *database.Tx) error {
			_, err := orm.Update(ctx, tx, f.Query(), f.Key(), label.Code, orm.TextSet(f.Name, "first"))
			return err
		})
		defer release()
		ready, done := make(chan int, 1), make(chan struct{})
		var result error
		go func() {
			defer close(done)
			result = c.within(ctx, func(tx *database.Tx) error {
				pid, err := auditBackendPID(ctx, tx)
				if err != nil {
					return err
				}
				ready <- pid
				_, err = orm.Update(ctx, tx, f.Query(), f.Key(), label.Code, orm.TextSet(f.Name, "second"))
				return err
			})
		}()
		defer func() { cancel(); <-done }()
		var waiter int
		select {
		case waiter = <-ready:
		case <-done:
			t.Fatal("second writer failed before readiness", result)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := waitAuditBlocker(ctx, c, blocker, waiter, done); err != nil {
			t.Fatal(err)
		}
		release()
		select {
		case <-done:
			if result != nil {
				t.Fatal(result)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := c.within(ctx, func(tx *database.Tx) error {
			stored, err := orm.RequireKey(ctx, tx, f.Table, label.Code)
			if err != nil {
				return err
			}
			if stored.Name != "second" {
				return errors.New("serialized update did not persist")
			}
			page, err := audit.ModelHistory(ctx, tx, c.recorder, auditqueries.LabelIdentity.ReferenceOf(label), orm.PageRequest{Number: 1, Size: 10})
			if err != nil {
				return err
			}
			if page.Total != 3 || len(page.Items) != 3 {
				return errors.New("serialized writes did not each record once")
			}
			for index, want := range []struct{ before, after string }{{"first", "second"}, {"original", "first"}} {
				field, err := orm.AuditRead(page.Items[index].Changes(), orm.TextExpr(f.Name))
				if err != nil {
					return err
				}
				change, ok := field.Get()
				if !ok {
					return errors.New("serialized audit omitted name")
				}
				before, err := change.Before().Get()
				if err != nil {
					return err
				}
				after, err := change.After().Get()
				if err != nil {
					return err
				}
				left, leftOK := before.Get()
				right, rightOK := after.Get()
				if !leftOK || !rightOK || left != want.before || right != want.after {
					return errors.New("serialized audit captured a stale before-image")
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAuditedLookupAndRejectedAdjustmentAllowKeyShare(t *testing.T) {
	for _, scenario := range []string{"update-or-create", "bound-rejected"} {
		t.Run(scenario, func(t *testing.T) {
			c := prepare(t, true)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			f := auditqueries.Accounts
			input := draft(t, "lookup-lock@example.test")
			var account auditqueries.Account
			if err := c.within(ctx, func(tx *database.Tx) error {
				var err error
				account, err = orm.Insert(ctx, tx, f.Table, input...)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			q := f.Query().Where(orm.KeyEq(f.Key(), account.ID))
			_, release := holdAuditTransaction(t, ctx, c, func(tx *database.Tx) error {
				_, err := orm.RequireLocked(ctx, tx, orm.ForKeyShare(q))
				return err
			})
			defer release()
			err := c.within(ctx, func(tx *database.Tx) error {
				if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
					return err
				}
				if scenario == "bound-rejected" {
					_, err := orm.Update(ctx, tx, q, f.Key(), account.ID, orm.NumberIncrement(f.Balance, decimal.FromInt64(1), orm.AtMost(account.Balance)))
					return err
				}
				_, err := orm.UpdateOrCreate(ctx, tx, q, input, func(context.Context, *database.Tx, auditqueries.Account) ([]orm.Assignment[auditqueries.Account], error) {
					return []orm.Assignment[auditqueries.Account]{orm.TextSet(f.PasswordHash, "lookup-changed")}, nil
				})
				return err
			})
			if scenario == "bound-rejected" {
				if !errors.Is(err, orm.ErrBoundExceeded) {
					t.Fatal("bound diagnosis blocked or lost its classification", err)
				}
			} else if err != nil {
				t.Fatal("lookup update blocked on KEY SHARE", err)
			}
			if err := c.within(ctx, func(tx *database.Tx) error {
				stored, err := orm.RequireKey(ctx, tx, f.Table, account.ID)
				if err != nil {
					return err
				}
				want := int64(2)
				if scenario == "bound-rejected" {
					want = 1
					if stored.Balance != account.Balance {
						return errors.New("rejected adjustment changed the balance")
					}
				} else if stored.PasswordHash != "lookup-changed" {
					return errors.New("lookup update did not persist")
				}
				if history(t, c, ctx, tx, account).Total != want {
					return errors.New("unexpected history for lookup/adjustment")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
