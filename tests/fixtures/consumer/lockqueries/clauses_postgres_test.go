package lockqueries_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPostgresIndependentInputLockStrengths(t *testing.T) {
	ctx, db, path := lockFixture(t)
	countries := rows.As[countryAlias](models.Countries.Query(), "country")
	locations := rows.As[locationAlias](models.Locations.Query(), "location")
	c, l := rows.Scope(countries), rows.Scope(locations)
	joined := rows.InnerJoin(countries, locations, rows.On(expr.At(c, models.Countries.Code), expr.At(l, models.Locations.CountryCode)))
	country := rows.LeftScope(joined, rows.Scope(countries))
	location := rows.RightScope(joined, rows.Scope(locations))
	selected := rows.LockRows(rows.SelectRecord(joined, country), rows.UpdateLock(rows.Target(country)), rows.KeyShareLock(rows.Target(location)))
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := selected.All(ctx, tx)
		return err
	})
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.Countries.Query().ForKeyShare().NoWait().Where(models.Countries.Code.Eq(models.CountryCode("A"))).Require(ctx, tx)
		return err
	}))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.Locations.Query().ForNoKeyUpdate().NoWait().Where(models.Locations.Code.Eq(models.LocationCode("one"))).Require(ctx, tx)
		return err
	}); err != nil {
		t.Fatal("key-share clause became a stronger lock", err)
	}
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.Locations.Query().ForUpdate().NoWait().Where(models.Locations.Code.Eq(models.LocationCode("one"))).Require(ctx, tx)
		return err
	}))
	finish()
}

func TestPostgresIndependentInputLockPolicies(t *testing.T) {
	for _, held := range []string{"country", "location"} {
		t.Run(held, func(t *testing.T) {
			ctx, db, path := lockFixture(t)
			countries := rows.As[countryAlias](models.Countries.Query(), "country")
			locations := rows.As[locationAlias](models.Locations.Query(), "location")
			c, l := rows.Scope(countries), rows.Scope(locations)
			joined := rows.InnerJoin(countries, locations, rows.On(expr.At(c, models.Countries.Code), expr.At(l, models.Locations.CountryCode)))
			country := rows.LeftScope(joined, rows.Scope(countries))
			location := rows.RightScope(joined, rows.Scope(locations))
			selected := rows.LockRows(rows.SelectRecord(joined, country), rows.UpdateLock(rows.Target(country)).SkipLocked(), rows.KeyShareLock(rows.Target(location)).NoWait())
			finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
				if held == "country" {
					_, err := models.Countries.Query().ForUpdate().Where(models.Countries.Code.Eq(models.CountryCode("A"))).Require(ctx, tx)
					return err
				}
				_, err := models.Locations.Query().ForUpdate().Where(models.Locations.Code.Eq(models.LocationCode("one"))).Require(ctx, tx)
				return err
			})
			defer finish()
			err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
				items, err := selected.All(ctx, tx)
				if err == nil && len(items) != 0 {
					return errors.New("conflicting join returned items")
				}
				return err
			})
			if held == "location" {
				assertLockError(t, err)
			} else if err != nil {
				t.Fatal("unrelated NOWAIT replaced country SKIP LOCKED", err)
			}
		})
	}
}

func TestPostgresOverlappingInputLockPrecedence(t *testing.T) {
	ctx, db, path := lockFixture(t)
	countries := rows.As[countryAlias](models.Countries.Query(), "country")
	scope := rows.Scope(countries)
	selected := rows.SelectRecord(countries, scope)
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := rows.LockRows(selected, rows.UpdateLock(rows.Target(scope)), rows.KeyShareLock(rows.Target(scope))).All(ctx, tx)
		return err
	})
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.Countries.Query().ForKeyShare().NoWait().Where(models.Countries.Code.Eq(models.CountryCode("A"))).Require(ctx, tx)
		return err
	}))
	finish()
	finish = holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := models.Countries.Query().ForUpdate().All(ctx, tx)
		return err
	})
	defer finish()
	for _, test := range []struct {
		name    string
		clauses []rows.RowLock[rows.Alias[countryAlias, models.Country]]
		noWait  bool
	}{
		{"nowait-then-skip", []rows.RowLock[rows.Alias[countryAlias, models.Country]]{rows.KeyShareLock(rows.Target(scope)).NoWait(), rows.UpdateLock(rows.Target(scope)).SkipLocked()}, true},
		{"skip-then-nowait", []rows.RowLock[rows.Alias[countryAlias, models.Country]]{rows.UpdateLock(rows.Target(scope)).SkipLocked(), rows.KeyShareLock(rows.Target(scope)).NoWait()}, true},
		{"skip-then-wait", []rows.RowLock[rows.Alias[countryAlias, models.Country]]{rows.UpdateLock(rows.Target(scope)).SkipLocked(), rows.ShareLock(rows.Target(scope))}, false},
		{"wait-then-skip", []rows.RowLock[rows.Alias[countryAlias, models.Country]]{rows.ShareLock(rows.Target(scope)), rows.UpdateLock(rows.Target(scope)).SkipLocked()}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
				items, err := rows.LockRows(selected, test.clauses...).All(ctx, tx)
				if err == nil && len(items) != 0 {
					return errors.New("overlapping SKIP LOCKED returned locked items")
				}
				return err
			})
			if test.noWait {
				assertLockError(t, err)
			} else if err != nil {
				t.Fatal("overlapping SKIP LOCKED lost precedence", err)
			}
		})
	}
}

func TestPostgresLockWindowValidationPreservesTransaction(t *testing.T) {
	ctx, db, path := lockFixture(t)
	q := models.Countries.Query()
	rank := expr.RowNumber(expr.WindowFor(rows.ModelScope(q)))
	calculation := expr.AddValue(rank, rank)
	invalid := rows.LockRows(rows.ReadScalar(rows.SelectScalar(rows.From(q), calculation)), rows.UpdateLock(rows.Target(rows.ModelScope(q))))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		if _, err := invalid.All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("invalid window lock was not rejected before SQL")
		}
		inner := rows.SelectScalar(rows.From(q.Limit(1)), calculation)
		outer := rows.LockRows(rows.ReadScalar(rows.SelectScalar(rows.From(q), rows.ScalarRowQuery(rows.From(q), inner).Value())), rows.UpdateLock(rows.Target(rows.ModelScope(q))))
		items, err := outer.All(ctx, tx)
		if err != nil {
			return err
		}
		if len(items) != 2 {
			return errors.New("independent window changed outer result count")
		}
		for _, row := range items {
			if n, present := row.Get(); !present || n != 2 {
				return errors.New("independent window calculation lost its value")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
