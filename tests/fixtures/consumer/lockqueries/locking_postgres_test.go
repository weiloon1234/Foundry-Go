package lockqueries_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func lockFixture(t *testing.T) (context.Context, *database.DB, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	db := pgtest.Open(t)
	path := `SET LOCAL search_path TO "` + pgtest.Namespace(t, db) + `"`
	if err := db.Transaction(ctx, func(tx *database.Tx) error {
		for _, statement := range []string{path,
			`CREATE TABLE countries (code text PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE locations (code text PRIMARY KEY,country_code text NOT NULL)`,
		} {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		if _, err := orm.InsertMany(ctx, tx, models.Countries.Table, [][]orm.Assignment[models.Country]{{orm.KeySet(models.Countries.Code, "A"), orm.TextSet(models.Countries.Name, "Alpha")}, {orm.KeySet(models.Countries.Code, "B"), orm.TextSet(models.Countries.Name, "Beta")}}); err != nil {
			return err
		}
		_, err := orm.Insert(ctx, tx, models.Locations.Table, orm.KeySet(models.Locations.Code, models.LocationCode("one")), orm.TextSet(models.Locations.CountryCode, models.CountryCode("A")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ctx, db, path
}

func inLockTransaction(ctx context.Context, db *database.DB, path string, read func(*database.Tx) error) error {
	return db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		return read(tx)
	})
}

// The readiness barrier fires after the read has acquired and closed its rows.
// Cleanup always releases and joins the holder, including assertion failures.
func holdLock(t *testing.T, ctx context.Context, db *database.DB, path string, read func(*database.Tx) error) func() {
	t.Helper()
	ready, stop, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
			if err := read(tx); err != nil {
				return err
			}
			close(ready)
			select {
			case <-stop:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("lock holder failed: %v", err)
	}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			close(stop)
			if err := <-done; err != nil {
				t.Error("lock holder finish", err)
			}
		})
	}
	t.Cleanup(finish)
	return finish
}

func assertLockError(t *testing.T, err error) {
	t.Helper()
	var failure *database.Error
	if !errors.As(err, &failure) || failure.SQLState() != "55P03" {
		t.Fatalf("expected PostgreSQL row contention, got %v", err)
	}
}

func TestPostgresLockedModelContentionAndRelease(t *testing.T) {
	ctx, db, path := lockFixture(t)
	lockedCountry := orm.Lock(models.Countries.Query())
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, lockedCountry.Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	})
	err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.FirstLocked(ctx, tx, lockedCountry.NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	})
	assertLockError(t, err)
	err = inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		missing, err := orm.FirstLocked(ctx, tx, lockedCountry.SkipLocked().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		if err != nil {
			return err
		}
		if missing.IsSet() {
			return errors.New("SkipLocked returned locked row")
		}
		items, err := orm.AllLocked(ctx, tx, lockedCountry.SkipLocked())
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].Code != "B" {
			return errors.New("SkipLocked lost available items")
		}
		_, err = orm.RequireLocked(ctx, tx, lockedCountry.SkipLocked().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		if !errors.Is(err, database.NotFound) {
			return errors.New("RequireFind missing contention result is not NotFound")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		first, err := orm.RequireLocked(ctx, tx, lockedCountry.NoWait())
		if err != nil {
			return err
		}
		if first.Code != "A" {
			return errors.New("commit did not release first row or natural-key ordering changed")
		}
		_, err = orm.Update(ctx, tx, models.Countries.Table.Query(), models.Countries.Table.Key(), first.Code, orm.TextSet(models.Countries.Name, first.Name+" updated"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLockStrengthCompatibility(t *testing.T) {
	for _, test := range []struct {
		name          string
		held, attempt orm.LockedQuery[models.Country]
		conflicts     bool
	}{
		{"share-share", orm.ForShare(models.Countries.Query()), orm.ForShare(models.Countries.Query()), false},
		{"share-no-key-update", orm.ForShare(models.Countries.Query()), orm.ForNoKeyUpdate(models.Countries.Query()), true},
		{"no-key-update-key-share", orm.ForNoKeyUpdate(models.Countries.Query()), orm.ForKeyShare(models.Countries.Query()), false},
		{"update-key-share", orm.Lock(models.Countries.Query()), orm.ForKeyShare(models.Countries.Query()), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, db, path := lockFixture(t)
			finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
				_, err := orm.RequireLocked(ctx, tx, test.held.Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
				return err
			})
			defer finish()
			err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
				_, err := orm.RequireLocked(ctx, tx, test.attempt.NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
				return err
			})
			if test.conflicts {
				assertLockError(t, err)
			} else if err != nil {
				t.Fatal("compatible lock blocked", err)
			}
		})
	}
}

func TestPostgresLockSavepointAndOffsetSemantics(t *testing.T) {
	ctx, db, path := lockFixture(t)
	rollback := errors.New("rollback lock savepoint")
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		err := tx.Transaction(ctx, func(nested *database.Tx) error {
			if _, err := orm.RequireLocked(ctx, nested, orm.Lock(models.Countries.Query()).Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A")))); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			return errors.New("savepoint rollback lost error")
		}
		return tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := orm.RequireLocked(ctx, nested, orm.Lock(models.Countries.Query()).Where(orm.KeyEq(models.Countries.Code, models.CountryCode("B"))))
			return err
		})
	})
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	}); err != nil {
		t.Fatal("rolled-back savepoint retained row lock", err)
	}
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("B"))))
		return err
	}))
	finish()
	finish = holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query().OrderBy(orm.KeyAsc(models.Countries.Code)).Offset(1)))
		return err
	})
	defer finish()
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	}))
}

func TestPostgresLockCancellationZeroLimitAndRecovery(t *testing.T) {
	ctx, db, path := lockFixture(t)
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	})
	defer finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		zero, err := orm.FirstLocked(ctx, tx, orm.Lock(models.Countries.Query().Limit(0)).NoWait())
		if err != nil {
			return err
		}
		if zero.IsSet() {
			return errors.New("zero limit selected a row")
		}
		err = tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := orm.RequireLocked(ctx, nested, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
			return err
		})
		assertLockError(t, err)
		_, err = orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("B"))))
		return err
	}); err != nil {
		t.Fatal("savepoint lock failure poisoned outer transaction", err)
	}
	err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := orm.RequireLocked(short, tx, orm.Lock(models.Countries.Query()).Wait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting lock ignored context deadline", err)
	}
}

type countryAlias struct{}
type locationAlias struct{}

func TestPostgresLockTypedJoinTargetsAndValues(t *testing.T) {
	ctx, db, path := lockFixture(t)
	countries := rows.As[countryAlias](models.Countries.Query(), "country")
	locations := rows.As[locationAlias](models.Locations.Query(), "location")
	c, l := rows.Scope(countries), rows.Scope(locations)
	joined := rows.LeftJoin(countries, locations, rows.On(expr.At(c, orm.KeyExpr(models.Countries.Code)), expr.At(l, orm.TextExpr(models.Locations.CountryCode))))
	countryScope := rows.LeftScope(joined, rows.Scope(countries))
	selected := rows.ForUpdate(rows.SelectRecord(joined, countryScope)).Of(rows.Target(countryScope))
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		items, err := selected.All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			return errors.New("locked outer join changed records")
		}
		return nil
	})
	defer finish()
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	}))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Locations.Query()).NoWait().Where(orm.KeyEq(models.Locations.Code, models.LocationCode("one"))))
		return err
	}); err != nil {
		t.Fatal("Of locked unrelated join side", err)
	}
	finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		values := rows.ReadScalar(rows.SelectScalar(rows.From(models.Countries.Query().OrderBy(orm.KeyAsc(models.Countries.Code))), expr.Row(expr.Value(orm.KeyExpr(models.Countries.Code))).Value()))
		codes, err := rows.ForKeyShare(values).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(codes) != 2 || codes[0] != "A" {
			return errors.New("locked scalar selection lost concrete codes")
		}
		if _, err := rows.ForUpdate(rows.SelectRecord(joined, countryScope)).All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("invalid nullable locking reached PostgreSQL")
		}
		_, err = orm.RequireLocked(ctx, tx, orm.ForShare(models.Countries.Query()))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLockedEagerRelationsAndStreaming(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := orm.Lock(models.Users.Query().With(models.UserLinks.Orders))
		first, err := orm.RequireLocked(t.Context(), tx, q.Where(orm.KeyEq(models.Users.ID, users[0].ID)))
		if err != nil {
			return err
		}
		if first.ID != users[0].ID {
			return errors.New("locked eager read changed parent")
		}
		if orders, loaded := first.Orders.Get(); !loaded || len(orders) != 2 {
			return errors.New("locked eager read failed to load relations after closing parent items")
		}
		if err := orm.EachLocked(t.Context(), tx, q, func(models.User) error { return nil }); !errors.Is(err, fault.Invalid) {
			return errors.New("locked stream silently ignored eager loading")
		}
		stopped := errors.New("stop locked stream")
		count := 0
		err = orm.EachLocked(t.Context(), tx, orm.Lock(models.Users.Query().Limit(1)), func(models.User) error { count++; return stopped })
		if !errors.Is(err, stopped) || count != 1 {
			return errors.New("locked stream lost callback error")
		}
		_, err = orm.RequireLocked(t.Context(), tx, orm.Lock(models.Users.Query()))
		return err
	})
}

func TestPostgresLockDerivedSourcesCTEPredicatesAndReadOnlyFailure(t *testing.T) {
	ctx, db, path := lockFixture(t)
	f := models.Countries
	derived := rows.As[countryAlias](models.Countries.Query().Where(orm.KeyEq(f.Code, models.CountryCode("A"))).Limit(1), "candidate")
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := rows.ForUpdate(rows.SelectRecord(derived, rows.Scope(derived))).Of(rows.Target(rows.Scope(derived))).RequireFirst(ctx, tx)
		return err
	})
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("A"))))
		return err
	}))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := orm.RequireLocked(ctx, tx, orm.Lock(models.Countries.Query()).NoWait().Where(orm.KeyEq(models.Countries.Code, models.CountryCode("B"))))
		return err
	}); err != nil {
		t.Fatal("derived filter locked an unrelated row", err)
	}
	finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		definition := rows.CTE("eligible", rows.ModelRecord(models.Countries.Query().Where(orm.KeyEq(f.Code, models.CountryCode("A")))))
		source := rows.AsRecord[countryAlias](definition, "eligible_rows")
		codes := rows.SelectScalar(source, expr.Row(expr.At(rows.Scope(source), orm.KeyExpr(models.Countries.Code))).Value())
		row, err := orm.RequireLocked(ctx, tx, orm.Lock(expr.Where(models.Countries.Query(), rows.InQuery(expr.Value(orm.KeyExpr(f.Code)), codes))))
		if err != nil {
			return err
		}
		if row.Code != "A" {
			return errors.New("locked CTE predicate lost its bindings")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, path); err != nil {
			return err
		}
		_, err := orm.RequireLocked(ctx, tx, orm.ForShare(models.Countries.Query()))
		return err
	}, database.TxOptions{ReadOnly: true})
	var failure *database.Error
	if !errors.As(err, &failure) || failure.SQLState() != "25006" {
		t.Fatal("read-only locking did not preserve PostgreSQL failure", err)
	}
}
