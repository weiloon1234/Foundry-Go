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
	"github.com/weiloon1234/Foundry-Go/database/query"
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
		if _, err := models.QueryCountries().CreateMany(ctx, tx, []models.CountryDraft{
			models.CountryDraft{}.SetCode(models.CountryCode("A")).SetName("Alpha"),
			models.CountryDraft{}.SetCode(models.CountryCode("B")).SetName("Beta"),
		}); err != nil {
			return err
		}
		_, err := models.QueryLocations().Create(ctx, tx, models.LocationDraft{}.SetCode(models.LocationCode("one")).SetCountryCode(models.CountryCode("A")))
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
	lockedCountry := models.QueryCountries().ForUpdate()
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := lockedCountry.RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	})
	err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := lockedCountry.NoWait().Find(ctx, tx, models.CountryCode("A"))
		return err
	})
	assertLockError(t, err)
	err = inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		missing, err := lockedCountry.SkipLocked().Find(ctx, tx, models.CountryCode("A"))
		if err != nil {
			return err
		}
		if missing.IsSet() {
			return errors.New("SkipLocked returned locked row")
		}
		rows, err := lockedCountry.SkipLocked().All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Code != "B" {
			return errors.New("SkipLocked lost available rows")
		}
		_, err = lockedCountry.SkipLocked().RequireFind(ctx, tx, models.CountryCode("A"))
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
		first, err := lockedCountry.NoWait().RequireFirst(ctx, tx)
		if err != nil {
			return err
		}
		if first.Code != "A" {
			return errors.New("commit did not release first row or natural-key ordering changed")
		}
		_, err = models.QueryCountries().Update(ctx, tx, first.Code, models.CountryDraft{}.SetName(first.Name+" updated"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLockStrengthCompatibility(t *testing.T) {
	for _, test := range []struct {
		name          string
		held, attempt models.CountryLockedQuery
		conflicts     bool
	}{
		{"share-share", models.QueryCountries().ForShare(), models.QueryCountries().ForShare(), false},
		{"share-no-key-update", models.QueryCountries().ForShare(), models.QueryCountries().ForNoKeyUpdate(), true},
		{"no-key-update-key-share", models.QueryCountries().ForNoKeyUpdate(), models.QueryCountries().ForKeyShare(), false},
		{"update-key-share", models.QueryCountries().ForUpdate(), models.QueryCountries().ForKeyShare(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, db, path := lockFixture(t)
			finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
				_, err := test.held.RequireFind(ctx, tx, models.CountryCode("A"))
				return err
			})
			defer finish()
			err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
				_, err := test.attempt.NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
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
			if _, err := models.QueryCountries().ForUpdate().RequireFind(ctx, nested, models.CountryCode("A")); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			return errors.New("savepoint rollback lost error")
		}
		return tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := models.QueryCountries().ForUpdate().RequireFind(ctx, nested, models.CountryCode("B"))
			return err
		})
	})
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	}); err != nil {
		t.Fatal("rolled-back savepoint retained row lock", err)
	}
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("B"))
		return err
	}))
	finish()
	finish = holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().OrderBy(models.CountryFields().Code.Asc()).Offset(1).ForUpdate().RequireFirst(ctx, tx)
		return err
	})
	defer finish()
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	}))
}

func TestPostgresLockCancellationZeroLimitAndRecovery(t *testing.T) {
	ctx, db, path := lockFixture(t)
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	})
	defer finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		zero, err := models.QueryCountries().Limit(0).ForUpdate().NoWait().First(ctx, tx)
		if err != nil {
			return err
		}
		if zero.IsSet() {
			return errors.New("zero limit selected a row")
		}
		err = tx.Transaction(ctx, func(nested *database.Tx) error {
			_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, nested, models.CountryCode("A"))
			return err
		})
		assertLockError(t, err)
		_, err = models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("B"))
		return err
	}); err != nil {
		t.Fatal("savepoint lock failure poisoned outer transaction", err)
	}
	err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := models.QueryCountries().ForUpdate().Wait().RequireFind(short, tx, models.CountryCode("A"))
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
	countries := query.As[countryAlias](models.QueryCountries(), "country")
	locations := query.As[locationAlias](models.QueryLocations(), "location")
	c, l := models.CountryFieldsAt(countries.Scope()), models.LocationFieldsAt(locations.Scope())
	joined := query.LeftJoin(countries, locations, query.On(c.Code, l.CountryCode))
	countryScope := query.LeftScope(joined, countries.Scope())
	selected := query.SelectRecord(joined, countryScope).ForUpdate().Of(countryScope)
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		rows, err := selected.All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			return errors.New("locked outer join changed records")
		}
		return nil
	})
	defer finish()
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	}))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryLocations().ForUpdate().NoWait().RequireFind(ctx, tx, models.LocationCode("one"))
		return err
	}); err != nil {
		t.Fatal("Of locked unrelated join side", err)
	}
	finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		values := query.SelectValue(models.QueryCountries(), models.CountryFields().Code.Value()).OrderBy(models.CountryFields().Code.Asc())
		codes, err := values.ForKeyShare().All(ctx, tx)
		if err != nil {
			return err
		}
		if len(codes) != 2 || codes[0] != "A" {
			return errors.New("locked scalar selection lost concrete codes")
		}
		if _, err := query.SelectRecord(joined, countryScope).ForUpdate().All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("invalid nullable locking reached PostgreSQL")
		}
		_, err = models.QueryCountries().ForShare().RequireFirst(ctx, tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLockedEagerRelationsAndStreaming(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, users []models.User) error {
		q := models.QueryUsers().With(models.UserRelations().Orders).ForUpdate()
		first, err := q.RequireFind(t.Context(), tx, users[0].ID)
		if err != nil {
			return err
		}
		if first.ID != users[0].ID {
			return errors.New("locked eager read changed parent")
		}
		if orders, loaded := first.Orders.Get(); !loaded || len(orders) != 2 {
			return errors.New("locked eager read failed to load relations after closing parent rows")
		}
		if err := q.Each(t.Context(), tx, func(models.User) error { return nil }); !errors.Is(err, fault.Invalid) {
			return errors.New("locked stream silently ignored eager loading")
		}
		stopped := errors.New("stop locked stream")
		count := 0
		err = models.QueryUsers().Limit(1).ForUpdate().Each(t.Context(), tx, func(models.User) error { count++; return stopped })
		if !errors.Is(err, stopped) || count != 1 {
			return errors.New("locked stream lost callback error")
		}
		_, err = models.QueryUsers().ForUpdate().RequireFirst(t.Context(), tx)
		return err
	})
}

func TestPostgresLockDerivedSourcesCTEPredicatesAndReadOnlyFailure(t *testing.T) {
	ctx, db, path := lockFixture(t)
	f := models.CountryFields()
	derived := query.As[countryAlias](models.QueryCountries().Where(f.Code.Eq(models.CountryCode("A"))).Limit(1), "candidate")
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := query.SelectRecord(derived, derived.Scope()).ForUpdate().Of(derived.Scope()).RequireFirst(ctx, tx)
		return err
	})
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	}))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().NoWait().RequireFind(ctx, tx, models.CountryCode("B"))
		return err
	}); err != nil {
		t.Fatal("derived filter locked an unrelated row", err)
	}
	finish()
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		definition := query.CTE("eligible", models.QueryCountries().Where(f.Code.Eq(models.CountryCode("A"))))
		source := query.As[countryAlias](definition, "eligible_rows")
		codes := query.SelectValue(source, models.CountryFieldsAt(source.Scope()).Code.Value())
		row, err := models.QueryCountries().Where(f.Code.InQuery(codes)).ForUpdate().RequireFirst(ctx, tx)
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
		_, err := models.QueryCountries().ForShare().RequireFirst(ctx, tx)
		return err
	}, database.TxOptions{ReadOnly: true})
	var failure *database.Error
	if !errors.As(err, &failure) || failure.SQLState() != "25006" {
		t.Fatal("read-only locking did not preserve PostgreSQL failure", err)
	}
}
