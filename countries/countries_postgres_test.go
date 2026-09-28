package countries

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/seed"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
)

func TestPostgresCountrySeedIsExplicitTypedAndPreservesApplicationFields(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	if rows, err := All(t.Context(), fixture.Store); err != nil || len(rows) != 0 {
		t.Fatal("countries silently seeded", err)
	}
	result, err := Seed(t.Context(), fixture.Store)
	if err != nil || result.Rows != BuiltinCount || result.Version != BuiltinVersion {
		t.Fatal("seed", result, err)
	}
	found, err := Find(t.Context(), fixture.Store, Code("MY"))
	country, ok := found.Get()
	if err != nil || !ok || country.ISO3 != "MYS" || country.Status != DisabledStatus || country.ReferenceVersion != BuiltinVersion {
		t.Fatal("typed country", err)
	}
	currencies, err := country.Currencies.Decode()
	if err != nil || len(currencies) != 1 || currencies[0].Code != "MYR" {
		t.Fatal("typed currencies", err)
	}
	if rows, err := Enabled(t.Context(), fixture.Store); err != nil || len(rows) != 0 {
		t.Fatal("seed activated countries", err)
	}
	rate, err := decimal.Parse("4.125")
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := QueryFoundryCountries().Update(ctx, tx, Code("MY"), CountryDraft{}.SetName("outdated").SetStatus(EnabledStatus).SetConversionRate(rate).SetIsDefault(true))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if result, err := Seed(t.Context(), fixture.Store); err != nil || result.Rows != BuiltinCount {
		t.Fatal("repeat seed", err)
	}
	found, err = Find(t.Context(), fixture.Store, Code("MY"))
	country, ok = found.Get()
	storedRate, present := country.ConversionRate.Get()
	if err != nil || !ok || country.Name != "Malaysia" || country.Status != EnabledStatus || !country.IsDefault || !present || storedRate != rate {
		t.Fatal("reference refresh overwrote application fields", err)
	}
	if rows, err := All(t.Context(), fixture.Store); err != nil || len(rows) != BuiltinCount {
		t.Fatal("seeding duplicated countries", err)
	}
	if rows, err := Enabled(t.Context(), fixture.Store); err != nil || len(rows) != 1 || rows[0].ISO2 != "MY" {
		t.Fatal("enabled query", err)
	}
	if exists, err := Exists(t.Context(), fixture.Store, Code("MY")); err != nil || !exists {
		t.Fatal("existence query", err)
	}
	if result, err := Find(t.Context(), fixture.Store, Code("ZZ")); err != nil || result.IsSet() {
		t.Fatal("absent country", err)
	}
}

func TestPostgresCountrySeederJoinsAndRollsBack(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	veto := errors.New("rollback")
	if err := fixture.DB.Transaction(t.Context(), func(tx *database.Tx) error {
		result, err := SeedIn(t.Context(), tx, fixture.Store)
		if err != nil {
			return err
		}
		if result.Rows != BuiltinCount {
			t.Fatal("provisional seed count")
		}
		return veto
	}); !errors.Is(err, veto) {
		t.Fatal(err)
	}
	if rows, err := All(t.Context(), fixture.Store); err != nil || len(rows) != 0 {
		t.Fatal("seed escaped parent rollback", err)
	}
	registry, err := seed.New(Seeder(fixture.Store))
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Run(t.Context(), fixture.DB, SeederID)
	if err != nil || len(result.Committed) != 1 {
		t.Fatal("ordinary seeder registry integration", err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if _, err := Seed(t.Context(), fixture.Store); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if rows, err := All(t.Context(), fixture.Store); err != nil || len(rows) != BuiltinCount {
		t.Fatal("concurrent seeds duplicated rows", err)
	}
}
func TestPostgresFailedCountrySeedLeavesNoPartialRows(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := tx.Exec(ctx, `ALTER TABLE foundry_countries ADD CONSTRAINT fixture_reject_last_country CHECK (iso2 <> 'ZW')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if result, err := Seed(t.Context(), fixture.Store); err == nil || result.Rows != 0 {
		t.Fatal("failed seed reported success")
	}
	if rows, err := All(t.Context(), fixture.Store); err != nil || len(rows) != 0 {
		t.Fatal("failed seed left partial rows", err)
	}
}
