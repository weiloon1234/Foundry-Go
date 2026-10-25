package models_test

import (
	"reflect"
	"testing"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
)

func TestPostgresSharedDeclarationRelationParity(t *testing.T) {
	queryfixture.Run(t, func(tx *database.Tx, _ []models.User) error {
		got, err := models.Users.Query().With(models.UserLinks.Introducer, models.UserLinks.Referrals, models.UserLinks.Orders, models.UserValues.OrderCount, models.UserValues.OrderTotal, models.UserValues.OrderAverage, models.UserValues.OrderMinimum, models.UserValues.OrderMaximum).OrderBy(models.Users.Email.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		links, values := models.UserRelations(), models.UserAggregates()
		want, err := models.QueryUsers().With(links.Introducer, links.Referrals, links.Orders, values.OrderCount, values.OrderTotal, values.OrderAverage, values.OrderMinimum, values.OrderMaximum).OrderBy(models.UserFields().Email.Asc()).All(t.Context(), tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("shared declarations changed relation slots or aggregate values")
		}
		return nil
	})
}
