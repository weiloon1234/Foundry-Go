package guideadapters_test

import "time"

import (
	"testing"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestGuideDeclarations(t *testing.T) {
	if err := orm.Check(append(Tables(), Accounts, models.Users)...); err != nil {
		t.Fatal(err)
	}
	if err := articleDeclaration().Validate(); err != nil {
		t.Fatal(err)
	}
	id, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreUser(models.User{ID: id})
	if err != nil || restored.Key() != id {
		t.Fatal("identity example did not preserve the key", err)
	}
	if _, err := withOrigin(t.Context(), Account{ID: 7, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := accountProvider(nil).Validate(); err != nil {
		t.Fatal(err)
	}
	factory, err := usersFactory()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Draft(t.Context()); err != nil {
		t.Fatal(err)
	}
}
