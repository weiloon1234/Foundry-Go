package httpmodels_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"reflect"
	"testing"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmodels"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestDeclaredBindingsPreserveLegacySQL(t *testing.T) {
	user, err := model.ParseID[models.User]("018f47aa-7c89-7f10-bf8f-9df04f572d86")
	if err != nil {
		t.Fatal(err)
	}
	order, err := model.ParseID[models.Order]("018f47aa-7c89-7f10-bf8f-9df04f572d87")
	if err != nil {
		t.Fatal(err)
	}
	db := &failingExecutor{cause: errors.New("stop at query")}
	for _, pair := range []struct {
		name         string
		old, current func() error
	}{
		{"UUID and scope", func() error {
			_, err := models.QueryUsers().Where(models.UserFields().Status.Eq(models.StatusActive)).Find(t.Context(), db, user)
			return err
		}, func() error {
			_, err := httpmodels.UserByID(db).Resolve(t.Context(), httpkernel.UserPath{User: user})
			return err
		}},
		{"natural key", func() error { _, err := models.QueryCountries().Find(t.Context(), db, "a/b + %"); return err }, func() error {
			_, err := httpmodels.CountryByCode(db).Resolve(t.Context(), httpmodels.CountryPath{Country: "a/b + %"})
			return err
		}},
		{"parent scope", func() error {
			_, err := models.QueryOrders().Where(models.OrderFields().BuyerID.Eq(user)).Find(t.Context(), db, order)
			return err
		}, func() error {
			_, err := httpmodels.OrderWithinUser(db).Resolve(t.Context(), httpmodels.OrderPath{User: user, Order: order})
			return err
		}},
	} {
		t.Run(pair.name, func(t *testing.T) {
			oldErr := pair.old()
			if !errors.Is(oldErr, db.cause) {
				t.Fatal(oldErr)
			}
			sql, args := db.statement, db.arguments
			currentErr := pair.current()
			if !errors.Is(currentErr, db.cause) {
				t.Fatal(currentErr)
			}
			if sql != db.statement || !reflect.DeepEqual(args, db.arguments) || !oracleEqual(t, "consumer/httpmodels/sql_parity_test/1", oracle.Snapshot([]any{sql, args, errors.Is(oldErr, db.cause)}), oracle.Snapshot([]any{db.statement, db.arguments, errors.Is(currentErr, db.cause)})) {
				t.Fatal("old/new SQL or bindings differ", sql, db.statement)
			}
		})
	}
}
