package consumer_test

import (
	"foundry.test/consumer/catalog"
	"foundry.test/consumer/catalog/status"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

func TestGeneratedConsumerPackageGraph(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	fields := catalog.ProductFields()
	if err := catalog.QueryProducts().Where(fields.Status.Eq(status.Available), fields.CreatorID.Eq(id)).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := catalog.QueryProducts().With(catalog.ProductRelations().Creator.With(models.UserRelations().Introducer)).Validate(); err != nil {
		t.Fatal(err)
	}
	draft := catalog.ProductDraft{}.SetCode("BOOK").SetStatus(status.Available).SetCreatorID(id)
	if value, set := draft.Status().Get(); !set || value != status.Available {
		t.Fatal("imported enum draft lost type/value")
	}
	if value, set := (catalog.Product{CreatorID: id}).CreatorDraft().ID().Get(); !set || value != id {
		t.Fatal("generated dependency unavailable in handwritten method")
	}
}
