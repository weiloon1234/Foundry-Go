package natural_test

import (
	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"testing"
)

type countryAlias struct{}
type locationAlias struct{}

func TestPostgresNaturalKeyJoins(t *testing.T) {
	queryfixture.RunJoins(t, func(tx *database.Tx, users []models.User) error {
		counter := &queryfixture.QueryCounter{Executor: tx}
		for _, sql := range []string{
			`CREATE TABLE countries (code text PRIMARY KEY,name text NOT NULL)`,
			`CREATE TABLE locations (code text PRIMARY KEY,country_code text NOT NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), sql); err != nil {
				return err
			}
		}
		// Natural keys keep their declared Go type through aliases and ON.
		if _, err := tx.Exec(t.Context(), `INSERT INTO countries VALUES ('MY','Malaysia'),('SG','Singapore')`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `INSERT INTO locations VALUES ('KL','MY')`); err != nil {
			return err
		}
		countries := query.As[countryAlias](models.QueryCountries(), "country")
		locations := query.As[locationAlias](models.QueryLocations(), "location")
		country := models.CountryFieldsAt(countries.Scope())
		location := models.LocationFieldsAt(locations.Scope())
		natural := query.InnerJoin(countries, locations, query.On(country.Code, location.CountryCode))
		nc := models.CountryFieldsAt(query.LeftScope(natural, countries.Scope()))
		nl := models.LocationFieldsAt(query.RightScope(natural, locations.Scope()))
		naturalReport := reports.ProjectLocationRow(natural).SelectCountryCode(nc.Code.Value()).SelectLocationCode(nl.Code.Value()).SelectCountryName(nc.Name.Value()).Query()
		naturalRows, err := naturalReport.All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(naturalRows) != 1 || naturalRows[0].CountryCode != models.CountryCode("MY") || naturalRows[0].LocationCode != models.LocationCode("KL") || naturalRows[0].CountryName != "Malaysia" {
			t.Error("joined natural keys lost declared value types")
		}
		return nil
	})
}
