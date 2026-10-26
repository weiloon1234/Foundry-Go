package catalog

import (
	"github.com/weiloon1234/Foundry-Go/tests/oracle"

	"testing"

	"foundry.test/consumer/catalog/status"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestProductORMParity(t *testing.T) {
	got, err := orm.Compile(Products.Query().Where(Products.Status.Eq(status.Available), orm.Has(ProductLinks.Creator)))
	if err != nil {
		t.Fatal(err)
	}

	if !oracleEqual(t, "consumer/catalog/orm_test/1", oracle.Snapshot([]any{got.SQL(), got.Arguments()})) {
		t.Fatal("frozen legacy comparison differs")
	}
}
