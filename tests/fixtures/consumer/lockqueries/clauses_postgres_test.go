package lockqueries_test

import (
	"errors"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestPostgresIndependentInputLockStrengths(t *testing.T) {
	ctx, db, path := lockFixture(t)
	countries := query.As[countryAlias](models.QueryCountries(), "country")
	locations := query.As[locationAlias](models.QueryLocations(), "location")
	c, l := models.CountryFieldsAt(countries.Scope()), models.LocationFieldsAt(locations.Scope())
	joined := query.InnerJoin(countries, locations, query.On(c.Code, l.CountryCode))
	country := query.LeftScope(joined, countries.Scope())
	location := query.RightScope(joined, locations.Scope())
	selected := query.SelectRecord(joined, country).LockRows(
		query.UpdateLock(country),
		query.KeyShareLock(location),
	)
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := selected.All(ctx, tx)
		return err
	})
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForKeyShare().NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	}))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryLocations().ForNoKeyUpdate().NoWait().RequireFind(ctx, tx, models.LocationCode("one"))
		return err
	}); err != nil {
		t.Fatal("key-share clause became a stronger lock", err)
	}
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryLocations().ForUpdate().NoWait().RequireFind(ctx, tx, models.LocationCode("one"))
		return err
	}))
	finish()
}

func TestPostgresIndependentInputLockPolicies(t *testing.T) {
	for _, held := range []string{"country", "location"} {
		t.Run(held, func(t *testing.T) {
			ctx, db, path := lockFixture(t)
			countries := query.As[countryAlias](models.QueryCountries(), "country")
			locations := query.As[locationAlias](models.QueryLocations(), "location")
			c, l := models.CountryFieldsAt(countries.Scope()), models.LocationFieldsAt(locations.Scope())
			joined := query.InnerJoin(countries, locations, query.On(c.Code, l.CountryCode))
			country := query.LeftScope(joined, countries.Scope())
			location := query.RightScope(joined, locations.Scope())
			selected := query.SelectRecord(joined, country).LockRows(
				query.UpdateLock(country).SkipLocked(),
				query.KeyShareLock(location).NoWait(),
			)
			finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
				if held == "country" {
					_, err := models.QueryCountries().ForUpdate().RequireFind(ctx, tx, models.CountryCode("A"))
					return err
				}
				_, err := models.QueryLocations().ForUpdate().RequireFind(ctx, tx, models.LocationCode("one"))
				return err
			})
			defer finish()
			err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
				rows, err := selected.All(ctx, tx)
				if err == nil && len(rows) != 0 {
					return errors.New("conflicting join returned rows")
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
	countries := query.As[countryAlias](models.QueryCountries(), "country")
	scope := countries.Scope()
	selected := query.SelectRecord(countries, scope)
	finish := holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := selected.LockRows(query.UpdateLock(scope), query.KeyShareLock(scope)).All(ctx, tx)
		return err
	})
	assertLockError(t, inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForKeyShare().NoWait().RequireFind(ctx, tx, models.CountryCode("A"))
		return err
	}))
	finish()
	finish = holdLock(t, ctx, db, path, func(tx *database.Tx) error {
		_, err := models.QueryCountries().ForUpdate().All(ctx, tx)
		return err
	})
	defer finish()
	for _, test := range []struct {
		name    string
		clauses []query.RowLock[query.Alias[countryAlias, models.Country]]
		noWait  bool
	}{
		{"nowait-then-skip", []query.RowLock[query.Alias[countryAlias, models.Country]]{query.KeyShareLock(scope).NoWait(), query.UpdateLock(scope).SkipLocked()}, true},
		{"skip-then-nowait", []query.RowLock[query.Alias[countryAlias, models.Country]]{query.UpdateLock(scope).SkipLocked(), query.KeyShareLock(scope).NoWait()}, true},
		{"skip-then-wait", []query.RowLock[query.Alias[countryAlias, models.Country]]{query.UpdateLock(scope).SkipLocked(), query.ShareLock(scope)}, false},
		{"wait-then-skip", []query.RowLock[query.Alias[countryAlias, models.Country]]{query.ShareLock(scope), query.UpdateLock(scope).SkipLocked()}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
				rows, err := selected.LockRows(test.clauses...).All(ctx, tx)
				if err == nil && len(rows) != 0 {
					return errors.New("overlapping SKIP LOCKED returned locked rows")
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
	q := models.QueryCountries()
	rank := query.RowNumber(query.WindowFor(q))
	calculation := query.AddValue(rank, rank)
	invalid := query.SelectValue(q, calculation).LockRows(query.UpdateLock(q.Scope()))
	if err := inLockTransaction(ctx, db, path, func(tx *database.Tx) error {
		if _, err := invalid.All(ctx, tx); !errors.Is(err, fault.Invalid) {
			return errors.New("invalid window lock was not rejected before SQL")
		}
		inner := query.SelectValue(q, calculation).Limit(1)
		outer := query.SelectValue(q, query.ScalarQuery(q, inner)).LockRows(query.UpdateLock(q.Scope()))
		rows, err := outer.All(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 2 {
			return errors.New("independent window changed outer result count")
		}
		for _, row := range rows {
			if n, present := row.Get(); !present || n != 2 {
				return errors.New("independent window calculation lost its value")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
