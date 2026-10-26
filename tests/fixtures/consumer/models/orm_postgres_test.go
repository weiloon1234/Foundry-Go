package models_test

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func TestPostgresSharedDeclarationRelationParity(t *testing.T) {
	queryfixture.RunStable(t, func(tx *database.Tx, _ []models.User) error {
		got, err := models.Users.Query().With(models.UserLinks.Introducer, models.UserLinks.Referrals, models.UserLinks.Orders, models.UserValues.OrderCount, models.UserValues.OrderTotal, models.UserValues.OrderAverage, models.UserValues.OrderMinimum, models.UserValues.OrderMaximum).OrderBy(models.Users.Email.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}

		if !oracleEqual(t, "consumer/models/postgres/hydration", oracle.ModelSnapshot(got)) {
			t.Fatal("shared declarations changed relation slots or aggregate values")
		}
		return nil
	})
}
