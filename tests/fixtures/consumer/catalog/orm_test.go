package catalog

import (
	"reflect"
	"testing"

	"foundry.test/consumer/catalog/status"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestProductORMParity(t *testing.T) {
	got, err := orm.Compile(Products.Query().Where(Products.Status.Eq(status.Available), orm.Has(ProductLinks.Creator)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := QueryProducts().Where(ProductFields().Status.Eq(status.Available), ProductRelations().Creator.Exists()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	if got.SQL() != want.SQL() || !reflect.DeepEqual(got.Arguments(), want.Arguments()) {
		t.Fatalf("product SQL changed\nnew: %s\nold: %s", got.SQL(), want.SQL())
	}
}
