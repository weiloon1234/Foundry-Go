package consumer_test

import "time"

import (
	"foundry.test/consumer/catalog"
	"foundry.test/consumer/catalog/status"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"testing"
)

func TestHandwrittenConsumerPackageGraph(t *testing.T) {
	id, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fields := catalog.Products
	if _, err := orm.Compile(catalog.Products.Query().Where(orm.Eq(fields.Status, status.Available), orm.Eq(fields.CreatorID, id))); err != nil {
		t.Fatal(err)
	}
	if _, err := orm.Compile(catalog.Products.Query().With(catalog.ProductLinks.Creator.With(models.UserLinks.Introducer))); err != nil {
		t.Fatal(err)
	}
	// These assignments are compiled in the importing package, without model emission.
	assigned := []orm.Assignment[catalog.Product]{orm.Set(fields.Code, "BOOK"), orm.Set(fields.Status, status.Available), orm.Set(fields.CreatorID, id)}
	inspectInsertAssignments(t, fields.Table, assigned, func(write *orm.Write[catalog.Product]) {
		if v, ok := orm.WriteGet(write, func() func(arg0 string) orm.Assignment[catalog.Product] {
			receiver := fields.Code
			return func(arg0 string) orm.Assignment[catalog.Product] { return orm.Set(receiver, arg0) }
		}()).Get(); !ok || v != "BOOK" {
			t.Error("natural-key input lost")
		}
		if v, ok := orm.WriteGet(write, func() func(arg0 status.Status) orm.Assignment[catalog.Product] {
			receiver := fields.Status
			return func(arg0 status.Status) orm.Assignment[catalog.Product] { return orm.Set(receiver, arg0) }
		}()).Get(); !ok || v != status.Available {
			t.Error("imported enum input lost")
		}
		if v, ok := orm.WriteGet(write, func() func(arg0 model.ID[models.User]) orm.Assignment[catalog.Product] {
			receiver := fields.CreatorID
			return func(arg0 model.ID[models.User]) orm.Assignment[catalog.Product] { return orm.Set(receiver, arg0) }
		}()).Get(); !ok || v != id {
			t.Error("imported model ID input lost")
		}
	})
}

// The importing package needs no generated model output to construct a typed key assignment.
func TestCrossPackageBusinessAssignmentHelper(t *testing.T) {
	id, err := model.NewIDAt[models.User](time.Now())
	if err != nil {
		t.Fatal(err)
	}
	assignment := (catalog.Product{CreatorID: id}).CreatorAssignment()
	inspectInsertAssignments(t, models.Users.Table, []orm.Assignment[models.User]{assignment}, func(write *orm.Write[models.User]) {
		if creator, set := orm.WriteGet(write, func() func(arg0 model.ID[models.User]) orm.Assignment[models.User] {
			receiver := models.Users.ID
			return func(arg0 model.ID[models.User]) orm.Assignment[models.User] { return orm.Set(receiver, arg0) }
		}()).Get(); !set || creator != id {
			t.Error("cross-package business method lost its typed identity")
		}
	})
}

func userIdentityAssignment(id model.ID[models.User]) orm.Assignment[models.User] {
	return orm.Set(models.Users.ID, id)
}
