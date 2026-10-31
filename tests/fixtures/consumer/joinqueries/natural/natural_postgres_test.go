package natural_test

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"foundry.test/consumer/reports"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
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
		countries := rows.As[countryAlias](models.Countries.Query(), "country")
		locations := rows.As[locationAlias](models.Locations.Query(), "location")
		country := rows.Scope(countries)
		location := rows.Scope(locations)
		natural := rows.InnerJoin(countries, locations, rows.On(expr.At(country, orm.KeyExpr(models.Countries.Code)), expr.At(location, orm.TextExpr(models.Locations.CountryCode))))
		nc := rows.LeftScope(natural, rows.Scope(countries))
		nl := rows.RightScope(natural, rows.Scope(locations))
		naturalReport := rows.Select(natural, reports.LocationRows, rows.MapSelected(reports.LocationRows.CountryCode, expr.AsSelected(expr.Row(expr.At(nc, orm.KeyExpr(models.Countries.Code))))), rows.MapSelected(reports.LocationRows.LocationCode, expr.AsSelected(expr.Row(expr.At(nl, orm.KeyExpr(models.Locations.Code))))), rows.MapSelected(reports.LocationRows.CountryName, expr.AsSelected(expr.Row(expr.At(nc, orm.TextExpr(models.Countries.Name))))))
		naturalRows, err := rows.Read(naturalReport).All(t.Context(), counter)
		if err != nil {
			return err
		}
		if len(naturalRows) != 1 || naturalRows[0].CountryCode != models.CountryCode("MY") || naturalRows[0].LocationCode != models.LocationCode("KL") || naturalRows[0].CountryName != "Malaysia" {
			t.Error("joined natural keys lost declared value types")
		}
		return nil
	})
}
