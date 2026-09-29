package countries

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPostgresOnlyOneDefaultCountry(t *testing.T) {
	fixture := extensiontest.Open(t, Migrations())
	if _, err := Seed(t.Context(), fixture.Store); err != nil {
		t.Fatal(err)
	}
	if found, err := Default(t.Context(), fixture.Store); err != nil || found.IsSet() {
		t.Fatal("seeding chose a default", err)
	}
	for _, code := range []Code{"MY", "SG", "SG"} {
		if err := SetDefault(t.Context(), fixture.Store, code); err != nil {
			t.Fatal(err)
		}
		found, err := Default(t.Context(), fixture.Store)
		country, ok := found.Get()
		if err != nil || !ok || country.ISO2 != code {
			t.Fatal("default country", code, err)
		}
	}
	if err := SetDefault(t.Context(), fixture.Store, "ZZ"); !errors.Is(err, database.NotFound) {
		t.Fatal("missing default country", err)
	}
	if found, err := Default(t.Context(), fixture.Store); err != nil || defaultCode(found) != "SG" {
		t.Fatal("failed SetDefault changed the default", err)
	}
	if err := fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
		_, err := QueryFoundryCountries().Update(ctx, tx, Code("MY"), CountryDraft{}.SetIsDefault(true))
		return err
	}); err == nil {
		t.Fatal("a second default country was stored")
	}
}

func TestPostgresSingleDefaultMigrationRefusesExistingDuplicates(t *testing.T) {
	definitions := Migrations()
	fixture := extensiontest.Open(t, definitions[:1])
	if _, err := Seed(t.Context(), fixture.Store); err != nil {
		t.Fatal(err)
	}
	apply := func(statement string) error {
		return fixture.Store.Write(t.Context(), func(ctx context.Context, tx *database.Tx) error {
			_, err := tx.Exec(ctx, statement)
			return err
		})
	}
	if err := apply(`UPDATE foundry_countries SET is_default = iso2 IN ('MY','SG')`); err != nil {
		t.Fatal(err)
	}
	migration := definitions[1]
	if migration.Key.ID != SingleDefaultCountry || len(migration.SQL) != 2 {
		t.Fatal("unexpected migration history")
	}
	if err := apply(migration.SQL[0]); err == nil {
		t.Fatal("migration silently accepted two defaults")
	}
	if err := apply(`UPDATE foundry_countries SET is_default = false WHERE iso2 = 'SG'`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range migration.SQL {
		if err := apply(statement); err != nil {
			t.Fatal(err)
		}
	}
	if found, err := Default(t.Context(), fixture.Store); err != nil || defaultCode(found) != "MY" {
		t.Fatal("existing single default changed", err)
	}
}
func defaultCode(found value.Optional[Country]) Code {
	country, _ := found.Get()
	return country.ISO2
}
