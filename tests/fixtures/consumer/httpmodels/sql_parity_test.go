package httpmodels_test

import (
	"errors"
	"testing"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/httpmodels"
	"foundry.test/consumer/models"
	modelbinding "github.com/weiloon1234/Foundry-Go/http/modelbinding"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
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
		name    string
		current func() error
	}{
		{"UUID and scope",
			func() error {
				_, err := modelbinding.Resolve(httpmodels.UserByID(db), t.Context(), httpkernel.UserPath{User: user})
				return err
			}},
		{"natural key", func() error {
			_, err := modelbinding.Resolve(httpmodels.CountryByCode(db), t.Context(), httpmodels.CountryPath{Country: "a/b + %"})
			return err
		}},
		{"parent scope",
			func() error {
				_, err := modelbinding.Resolve(httpmodels.OrderWithinUser(db), t.Context(), httpmodels.OrderPath{User: user, Order: order})
				return err
			}},
	} {
		t.Run(pair.name, func(t *testing.T) {
			currentErr := pair.current()
			if !errors.Is(currentErr, db.cause) {
				t.Fatal(currentErr)
			}
			if !oracleEqual(t, "consumer/httpmodels/sql_parity_test/1", oracle.Snapshot([]any{db.statement, db.arguments, errors.Is(currentErr, db.cause)})) {
				t.Fatal("frozen SQL or bindings differ")
			}
		})
	}
}
