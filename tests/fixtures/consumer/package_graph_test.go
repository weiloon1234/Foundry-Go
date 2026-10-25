package consumer_test

import (
	"foundry.test/consumer/catalog"
	"foundry.test/consumer/catalog/status"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

func TestHandwrittenConsumerPackageGraph(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	fields := catalog.Products
	if _, err := orm.Compile(catalog.Products.Query().Where(fields.Status.Eq(status.Available), fields.CreatorID.Eq(id))); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Compile(catalog.Products.Query().With(catalog.ProductLinks.Creator.With(models.UserLinks.Introducer))); err != nil {
		t.Fatal(err)
	}
	// These assignments are compiled in the importing package, without model emission.
	assigned := []orm.Assignment[catalog.Product]{fields.Code.Set("BOOK"), fields.Status.Set(status.Available), fields.CreatorID.Set(id)}
	inspectInsertAssignments(t, fields.Table, assigned, func(write *orm.Write[catalog.Product]) {
		if v, ok := orm.WriteGet(write, fields.Code.Set).Get(); !ok || v != "BOOK" {
			t.Error("natural-key input lost")
		}
		if v, ok := orm.WriteGet(write, fields.Status.Set).Get(); !ok || v != status.Available {
			t.Error("imported enum input lost")
		}
		if v, ok := orm.WriteGet(write, fields.CreatorID.Set).Get(); !ok || v != id {
			t.Error("imported model ID input lost")
		}
	})
}

// The importing package needs no generated model output to construct a typed key assignment.
func TestCrossPackageBusinessDraftHelper(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	if creator, set := (catalog.Product{CreatorID: id}).CreatorDraft().ID().Get(); !set || creator != id {
		t.Fatal("cross-package business method lost its typed identity")
	}
}

func userIdentityAssignment(id model.ID[models.User]) orm.Assignment[models.User] {
	return models.Users.ID.Set(id)
}
