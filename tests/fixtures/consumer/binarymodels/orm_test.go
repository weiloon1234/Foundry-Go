package binarymodels_test

import (
	"testing"

	"foundry.test/consumer/binarymodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(binarymodels.Records.Table); err != nil {
		t.Fatal(err)
	}
	if err := rows.Check(binarymodels.Views.Row); err != nil {
		t.Fatal(err)
	}
}
