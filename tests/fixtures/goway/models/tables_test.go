package models_test

import (
	"testing"

	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestTablesReturnsOwnedDeclarations(t *testing.T) {
	first, second := models.Tables(), models.Tables()
	if len(first) != 3 || len(second) != 3 {
		t.Fatal("missing registered declarations")
	}
	first[0] = nil
	if err := orm.Check(second...); err != nil {
		t.Fatal("changing one registration list affected another", err)
	}
	if err := orm.Check(first...); err == nil {
		t.Fatal("missing declaration accepted")
	}
}
