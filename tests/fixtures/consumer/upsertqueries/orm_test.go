package upsertqueries_test

import (
	"testing"

	"foundry.test/consumer/upsertqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(upsertqueries.ExcludedRecords.Table); err != nil {
		t.Fatal(err)
	}
}
